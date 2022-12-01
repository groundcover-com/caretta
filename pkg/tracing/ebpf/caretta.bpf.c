#include <vmlinux.h>
#include <bpf_helpers.h>
#include <bpf_core_read.h>
#include <bpf_tracing.h>

char __license[] SEC("license") = "Dual MIT/GPL";

#define MAX_CONNECTIONS 10000000
#define DEBUG

#ifdef DEBUG
#define DEBUG_TEST 1
#else
#define DEBUG_TEST 0
#endif

#define debug_print(msg, ...) \
            do { if (DEBUG_TEST) bpf_printk(msg, __VA_ARGS__); } while (0)

enum connection_role {
    CONNECTION_ROLE_UNKNOWN = 0,
    CONNECTION_ROLE_CLIENT,
    CONNECTION_ROLE_SERVER,
};

static u32 global_id_counter = 0; 

// partial struct of args for tcp_set_state
struct set_state_args  {
    u64 padding;
    struct sock* skaddr;
    u32 oldstate;
    u32 newstate;
    // more...
};

// describing two sides of a connection. constant for each connection.
struct connection_tuple {
    __be32 src_ip;
    __be32 dst_ip;
    u16 src_port;
    u16 dst_port;
};

// all information needed to identify a specific connection.
// due to socket reuses, each of the members (beside id) may change while maintaing the others.
struct connection_identifier {
    u32 id; // program-genetated unique
    u32 pid;
    struct connection_tuple tuple;
    enum connection_role role;
   
};

// dynamic information about the state of a connection.
struct connection_throughput_stats {
    u64 bytes_sent;
    u64 bytes_received;
    u64 is_active; // u64 because it will be padded anyway. should change whether new members are added
};

// internal kernel-only struct to hold socket information which can't be parsed from struct sock.
struct sock_info  {
    u32 pid;
    enum connection_role role;
    u32 is_active;
    u32 id;
};



// internal kernel-only map to hold state for each sock observed.
struct bpf_map_def SEC("maps") sock_infos = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(struct sock*),
    .value_size = sizeof(struct sock_info),
    .max_entries = MAX_CONNECTIONS,
};

// the main product of the tracing - map containing all connections observed,
// with metadata and throughput stats.
// key is a whole identifier struct and not a single id to split the constant and dynamic values
// and to resemble as closely as possible the end result in the userspace code.
struct bpf_map_def SEC("maps") connections = {
    .type = BPF_MAP_TYPE_HASH,
    .key_size = sizeof(struct connection_identifier),
    .value_size = sizeof(struct connection_throughput_stats),
    .max_entries = MAX_CONNECTIONS,
};


// function for parsing the struct sock
static inline int parse_sock_data(struct sock* sock, struct connection_tuple* out_tuple, struct connection_throughput_stats* out_throughput) {

    if (sock == NULL) {
        debug_print("invalid sock received");
        return -1;
    }

    // struct sock wraps struct tcp_sock and struct inet_sock as its first member
    struct tcp_sock* tcp = (struct tcp_sock*)sock;
    struct inet_sock* inet = (struct inet_sock*)sock;

    // initialize variables. IP addresses and ports are read originally big-endian,
    // and we will convert the ports to little-endian.
    int err = 0;
    __be32 src_addr = 0;
    __be32 dst_addr = 0;
    __be16 src_port_be = 0;
    __be16 dst_port_be = 0;
    u16 src_port = 0;
    u16 dst_port = 0;

    // TODO finish parsing

    return 0;
};

// probing the tcp_data_queue kernel function, and adding the connection observed to the map.
SEC("kprobe/tcp_data_queue")
int handle_tcp_data_queue(struct pt_regs* ctx) {

    // first argument to tcp_data_queue is a struct sock*
    struct sock* sock = (struct sock*)PT_REGS_PARM1_CORE(ctx);

    struct connection_identifier conn_id;
    memset(&conn_id, 0, sizeof(conn_id));
    struct connection_throughput_stats throughput;
    memset(&throughput, 0, sizeof(throughput));

    if (parse_sock_data(sock, &conn_id.tuple, &throughput) == -1) {
        debug_print("error parsing sock");
        return -1;
    }

    // fill the conn_id extra details from sock_info map entry, or create one
    struct sock_info* sock_info = bpf_map_lookup_elem(&sock_infos, &sock);
    if (sock_info == 0) {
        // first time we encounter this sock
        // check if server or client and insert to the maps

        // the max_ack_backlog holds the limit for the accept queue
        // if it is a server, it will not be 0
        
        int max_ack_backlog = 0;
        bpf_core_read(&max_ack_backlog, sizeof(max_ack_backlog), &sock->sk_max_ack_backlog);

        struct sock_info info = {
            .pid = 0, //can't associate to pid anyway
            .role = max_ack_backlog == 0 ? CONNECTION_ROLE_CLIENT : CONNECTION_ROLE_SERVER,
            .is_active = true,
            .id = global_id_counter++,
        };
        bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);

        conn_id.pid = info.pid;
        conn_id.id = info.id;
        conn_id.role = info.role;
        throughput.is_active = true;

    } else {
        conn_id.pid = sock_info->pid;
        conn_id.id = sock_info->id;
        conn_id.role = sock_info->role;
        throughput.is_active = sock_info->is_active; // maybe set it to true?
    }

    bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);

    return 0;

};

SEC("tracepoint/sock/inet_sock_set_state")
int handle_sock_set_state(struct set_state_args* args) {
    
    struct sock* sock = (struct sock*)args->skaddr;

    // handle according the new state
    if (args->newstate == TCP_SYN_RECV) {
        // this is a server got syn after listen
        struct connection_identifier conn_id;
        memset(&conn_id, 0, sizeof(conn_id));
        struct connection_throughput_stats throughput;
        memset(&throughput, 0, sizeof(throughput));

        if (parse_sock_data(args->skaddr, &conn_id.tuple, &throughput) == -1) {
            return -1;
        }

        struct sock_info info = {
            .pid = 0, // can't associate to process
            .role = CONNECTION_ROLE_SERVER,
            .is_active = true,
            .id = global_id_counter++,
        };

        bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);

        conn_id.pid = info.pid;
        conn_id.id = info.id;
        conn_id.role = info.role;

        bpf_map_update_elem(&connections, &conn_id, &throughput, BPF_ANY);
             
    } else if (args->newstate == TCP_SYN_SENT) {
        // start of a client session
        u32 pid = bpf_get_current_pid_tgid() >> 32;
        
        struct sock_info info = {
            .pid = pid,
            .role = CONNECTION_ROLE_CLIENT,
            .is_active = true,
            .id = global_id_counter++,
        };

        bpf_map_update_elem(&sock_infos, &sock, &info, BPF_ANY);
    } else if(args->newstate == TCP_CLOSE || args->newstate == TCP_CLOSE_WAIT) {
        // mark as inactive
        struct sock_info* info = bpf_map_lookup_elem(&sock_infos, &sock);
        if (info != 0) {
            info->is_active = false;
        }
    }
    // TODO consider adding handler for TCP_FIN_WAIT

    return 0;
}