#ifndef __EBPF_UTILS_H__
#define __EBPF_UTILS_H__

#define BPF_SUCCESS 0
#define BPF_ERROR -1

#define DEBUG
#ifdef DEBUG
#define DEBUG_TEST 1
#else
#define DEBUG_TEST 0
#endif

#define debug_print(...)                                                       \
  do {                                                                         \
    if (DEBUG_TEST)                                                            \
      bpf_printk(__VA_ARGS__);                                                 \
  } while (0)


#endif