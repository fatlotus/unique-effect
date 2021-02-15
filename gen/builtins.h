#ifndef __BUILTINS_H__
#define __BUILTINS_H__

#include <stdbool.h>

typedef void *val_t;
typedef struct {
  val_t value;
  bool ready;
} future_t;

typedef void (*func_t)();

typedef struct {
  void *state;
  func_t func;
} closure_t;

struct hang10_runtime {
  closure_t upcoming_calls[100];
  int next_call;
  int current_call;
  closure_t after_delay[20];
  int next_delay;
};

struct hang10_sleep_state {
  future_t r[1];
  future_t *result[1];
  closure_t caller;
  bool conditions[1]; // needed for calling convention
};

void hang10_runtime_schedule(struct hang10_runtime *rt, closure_t closure);

#endif