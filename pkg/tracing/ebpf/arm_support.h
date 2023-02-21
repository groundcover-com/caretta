#ifndef __ARM_SUPPORT_H__
#define __ARM_SUPPORT_H__

struct user_pt_regs {
  __u64 regs[31];
  __u64 sp;
  __u64 pc;
  __u64 pstate;
};

#endif