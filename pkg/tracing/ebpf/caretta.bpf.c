#include "vmlinux.h"
#include <bpf_core_read.h>
#include <bpf_helpers.h>
#include <bpf_tracing.h>
#include "ebpf_utils.h"
#include "epbf_shared_types.h"
#include "ebpf_internel_types.h"

char __license[] SEC("license") = "Dual MIT/GPL";

// internal kernel-only map to hold state for each sock observed.
struct bpf_map_def SEC("maps") sock_infos = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(struct sock *),
    .value_size = sizeof(struct sock_info),
    .max_entries = MAX_CONNECTIONS,
};

// the main product of the tracing - map containing all connections observed,
// with metadata and throughput stats.
// key is a whole identifier struct and not a single id to split the constant
// and dynamic values and to resemble as closely as possible the end result in
// the userspace code.
struct bpf_map_def SEC("maps") connections = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(struct connection_identifier),
    .value_size = sizeof(struct connection_throughput_stats),
    .max_entries = MAX_CONNECTIONS,
};

// helper to convert short int from BE to LE
static inline u16 be_to_le(__be16 be) { return (be >> 8) | (be << 8); }

static inline u32 get_unique_id() {
  return bpf_ktime_get_ns() % __UINT32_MAX__; // no reason to use 64 bit for this
}

// function for parsing the struct sock
static inline int
parse_sock_data(struct sock *sock, struct connection_tuple *out_tuple,
                struct connection_throughput_stats *out_throughput) {

  if (sock == NULL) {
    debug_print("invalid sock received");
    return BPF_ERROR;
  }

  // struct sock wraps struct tcp_sock and struct inet_sock as its first member
  struct tcp_sock *tcp = (struct tcp_sock *)sock;
  struct inet_sock *inet = (struct inet_sock *)sock;

  // initialize variables. IP addresses and ports are read originally
  // big-endian, and we will convert the ports to little-endian.
  __be16 src_port_be = 0;
  __be16 dst_port_be = 0;

  // read connection tuple

  if (0 != bpf_core_read(&out_tuple->src_ip, sizeof(out_tuple->src_ip),
                      &inet->inet_saddr)) {
    debug_print("Error reading source ip");
    return BPF_ERROR;
  }

  if (0 != bpf_core_read(&out_tuple->dst_ip, sizeof(out_tuple->dst_ip),
                      &inet->inet_daddr)) {
    debug_print("Error reading dest ip");
    return BPF_ERROR;
  }

  if (0 != bpf_core_read(&src_port_be, sizeof(src_port_be), &inet->inet_sport)) {
    debug_print("Error reading src port");
    return BPF_ERROR;
  }
  out_tuple->src_port = be_to_le(src_port_be);

  if (0 != bpf_core_read(&dst_port_be, sizeof(dst_port_be), &inet->inet_dport)) {
    debug_print("Error reading dst port");
    return BPF_ERROR;
  }
  out_tuple->dst_port = be_to_le(dst_port_be);

  // read throughput data

  if (0 != bpf_core_read(&out_throughput->bytes_received,
                      sizeof(out_throughput->bytes_received),
                      &tcp->bytes_received)) {
    debug_print("Error reading bytes_received");
    return BPF_ERROR;
  }
  if (0 != bpf_core_read(&out_throughput->bytes_sent,
                      sizeof(out_throughput->bytes_sent), &tcp->bytes_sent)) {
    debug_print("Error reading bytes_sent");
    return BPF_ERROR;
  }

  return BPF_SUCCESS;
};

static inline enum connection_role get_sock_role(struct sock* sock) {
  // the max_ack_backlog holds the limit for the accept queue
  // if it is a server, it will not be 0
  int max_ack_backlog = 0;
  if (0 != bpf_core_read(&max_ack_backlog, sizeof(max_ack_backlog),
                &sock->sk_max_ack_backlog)) {
    return CONNECTION_ROLE_UNKNOWN;
  }

  return max_ack_backlog == 0 ? CONNECTION_ROLE_CLIENT : CONNECTION_ROLE_SERVER;      
}

// probing the tcp_data_queue kernel function, and adding the connection
// observed to the map.
SEC("kprobe/tcp_data_queue")
static int handle_tcp_data_queue(struct pt_regs *ctx) {
  // first argument to tcp_data_queue is a struct sock*
  struct sock *sock = (struct sock *)PT_REGS_PARM1_CORE(ctx);

  struct connection_identifier conn_id = {};
  struct connection_throughput_stats throughput = {};

  if (parse_sock_data(sock, &conn_id.tuple, &throughput) == BPF_ERROR) {
    debug_print("error parsing sock");
    return BPF_ERROR;
  }

  // skip unconnected sockets
  if (conn_id.tuple.dst_port == 0 && conn_id.tuple.dst_ip == BPF_SUCCESS) {
    return BPF_SUCCESS;
  }

  // fill the conn_id extra details from sock_info map entry, or create one
  struct sock_info *sock_info = bpf_map_lookup_elem(&sock_infos, &sock);
  if (sock_info == NULL) {
    // first time we encounter this sock
    // check if server or client and insert to the maps

    enum connection_role role = get_sock_role(sock);

    struct sock_info info = {
        .pid = 0, // can't associate to pid anyway
        .role = role,
        .is_active = true,
        .id = get_unique_id(),
    };
    bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);

    conn_id.pid = info.pid;
    conn_id.id = info.id;
    conn_id.role = info.role;
    throughput.is_active = true;

    bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);

    return BPF_SUCCESS;

  } 

  conn_id.pid = sock_info->pid;
  conn_id.id = sock_info->id;
  conn_id.role = sock_info->role;
  if (!sock_info->is_active) {
    debug_print("inactive sock in tcp_data_queue");
    return -1;
  }
  throughput.is_active = sock_info->is_active; 
  
  bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);

  return BPF_SUCCESS;
};

static inline int handle_set_tcp_syn_sent(struct sock* sock) {
  // start of a client session
  u32 pid = bpf_get_current_pid_tgid() >> 32;

  struct sock_info info = {
      .pid = pid,
      .role = CONNECTION_ROLE_CLIENT,
      .is_active = true,
      .id = get_unique_id(),
  };

  bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);

  return BPF_SUCCESS;
}

static inline int handle_set_tcp_syn_recv(struct sock* sock) {
  // this is a server getting syn after listen
    struct connection_identifier conn_id = {};
    struct connection_throughput_stats throughput = {};

    if (parse_sock_data(sock, &conn_id.tuple, &throughput) == BPF_ERROR) {
      return BPF_ERROR;
    }

    struct sock_info info = {
        .pid = 0, // can't associate to process
        .role = CONNECTION_ROLE_SERVER,
        .is_active = true,
        .id = get_unique_id(),
    };

    bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);

    // probably the dst ip will still be unitialized
    if (conn_id.tuple.dst_ip == 0) {
      return BPF_SUCCESS;
    }

    conn_id.pid = info.pid;
    conn_id.id = info.id;
    conn_id.role = info.role;

    bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);

    return BPF_SUCCESS;
}

static inline int handle_set_tcp_close(struct sock* sock) {
  // mark as inactive
  struct connection_identifier conn_id = {};
  struct connection_throughput_stats throughput = {};

  if (parse_sock_data(sock, &conn_id.tuple, &throughput) == BPF_ERROR) {
    return BPF_ERROR;
  }

  struct sock_info *info = bpf_map_lookup_elem(&sock_infos, &sock);
  if (info == NULL) {
    conn_id.id = get_unique_id();
    conn_id.pid = 0; // cannot associate to PID in this state
    conn_id.role = get_sock_role(sock);
  } else {
    conn_id.id = info->id;
    conn_id.pid = info->pid;
    conn_id.role = info->role;
    bpf_map_delete_elem(&sock_infos, &sock);
  }

  throughput.is_active = false;
  bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);

  return BPF_SUCCESS;
}

SEC("tracepoint/sock/inet_sock_set_state")
static int handle_sock_set_state(struct set_state_args *args) {
  struct sock *sock = (struct sock *)args->skaddr;

  switch(args->newstate) {
    case TCP_SYN_RECV: {
      return handle_set_tcp_syn_recv(sock) == BPF_ERROR;
    }
    case TCP_SYN_SENT: {
      return handle_set_tcp_syn_sent(sock) == BPF_ERROR;
    }
    case TCP_CLOSE:  {
      return handle_set_tcp_close(sock);
    }
  }

  return BPF_SUCCESS;
}
