/*
 * Copyright 2021 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include <assert.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#ifdef USE_LIBUV
#include <uv.h>
#endif

#include "builtins.h"

val_t kSingletonStream = (void *)40;
val_t kSingletonClock = (void *)50;
val_t kSingletonFileSystem = (void *)60;
val_t kSingletonFileSystemWillFail = (void *)70;

void unique_effect_runtime_schedule(struct unique_effect_runtime *rt,
                                    closure_t closure) {
  assert(closure.func != NULL);
  assert(rt->next_call < 100);

  // Ignore duplicated calls to schedule the same function, as they can result
  // in use-after-free bugs.
  for (int i = rt->current_call; i < rt->next_call; i++) {
    if (rt->upcoming_calls[i].state == closure.state) {
      fprintf(stderr, "eliding duplicated call %p\n", closure.state);
      return;
    }
  }

  rt->upcoming_calls[rt->next_call] = closure;
  rt->next_call++;
}

void unique_effect_print(struct unique_effect_runtime *rt, val_t console,
                         val_t msg, val_t *console_out) {
  assert(console == kSingletonStream);
  printf("%0.1fs %s\n", rt->current_time, (char *)msg);
  *console_out = console;
}

static void finish_current_iteration(struct unique_effect_runtime *rt) {
  for (; rt->current_call < rt->next_call; rt->current_call++) {
    rt->upcoming_calls[rt->current_call].func(
        rt, rt->upcoming_calls[rt->current_call].state);
  }
}

#ifdef USE_LIBUV
struct sleep_uv_adapter {
  struct unique_effect_runtime *runtime;
  struct unique_effect_sleep_state *call_state;
  uv_timer_t timer;
  double trigger_time;
};

static void sleep_adapter_result(uv_timer_t *timer) {
  struct sleep_uv_adapter *adapter = (struct sleep_uv_adapter *)timer->data;
  struct unique_effect_runtime *runtime = adapter->runtime;

  runtime->current_time = adapter->trigger_time;

  adapter->call_state->result[0]->value = kSingletonClock;
  adapter->call_state->result[0]->ready = true;

  unique_effect_runtime_schedule(runtime, adapter->call_state->caller);

  free(adapter->call_state);
  free(adapter);

  finish_current_iteration(runtime);
}

static void cancel_sleep(struct unique_effect_runtime *runtime, future_t *val) {
  // Cancellation reuses the intermediate values before setting ready = true.
  assert(val->ready == false);
  assert(val->value != kSingletonClock);

  if (val->value == NULL)
    return;

  struct sleep_uv_adapter *adapter = (struct sleep_uv_adapter *)val->value;
  val->value = kSingletonClock;
  val->ready = true;

  assert(uv_timer_stop(&adapter->timer) == 0);

  adapter->call_state->result[0]->value = kSingletonClock;
  adapter->call_state->result[0]->ready = true;

  unique_effect_runtime_schedule(runtime, adapter->call_state->caller);

  free(adapter->call_state);
  free(adapter);
}
#else
static void cancel_sleep(struct unique_effect_runtime *runtime, future_t *val) {
  assert(val->ready == false);
  assert(val->value != kSingletonClock);

  if (val->value == NULL)
    return;

  struct unique_effect_sleep_state **state_ptr =
      (struct unique_effect_sleep_state **)val->value;

  struct unique_effect_sleep_state *state = *state_ptr;

  state->result[0]->value = kSingletonClock;
  state->result[0]->ready = true;

  unique_effect_runtime_schedule(runtime, state->caller);

  // Cancel the pending timer.
  *state_ptr = NULL;
  free(state);
}
#endif

void unique_effect_sleep(struct unique_effect_runtime *rt,
                         struct unique_effect_sleep_state *state) {
  // Make sure the previous operation has finished. If not, simply pass through
  // the timer from before.
  if (!state->r[0].ready) {
    *state->result[0] = state->r[0];
    return;
  }

  // Make sure that repeated calls are ignored, emulating a user function.
  if (state->conditions[0]) {
    return;
  }
  state->conditions[0] = true;

#ifdef USE_LIBUV
  struct sleep_uv_adapter *adapter = malloc(sizeof(struct sleep_uv_adapter));
  adapter->call_state = state;
  adapter->timer.data = adapter;
  adapter->runtime = rt;
  adapter->trigger_time = rt->current_time + 1.0;

  // Store an intermediate value in case we need to cancel this clock.
  state->result[0]->value = adapter;

  uv_timer_init(uv_default_loop(), &adapter->timer);
  uv_timer_start(&adapter->timer, &sleep_adapter_result, 100, 0);
#else
  assert(state->r[0].value == kSingletonClock);
  assert(rt->next_timer < 20);

  // Register the new timer in the runtime.
  // rt->after_delay[rt->next_delay] = state->caller;
  // rt->after_delay_futures[rt->next_delay] = state->result[0];
  rt->timers[rt->next_timer] = state;

  // Store an intermediate value in case we need to cancel this clock, and wake
  // up the caller to let them know about it.
  state->result[0]->value = &rt->timers[rt->next_timer];
  unique_effect_runtime_schedule(rt, state->caller);

  rt->next_timer++;
#endif
}

void unique_effect_ReadLine(struct unique_effect_runtime *rt, val_t console,
                            val_t *console_out, val_t *name_out) {
  assert(console == kSingletonStream);
  *name_out = strdup("World");
  *console_out = console;
}

void unique_effect_itoa(struct unique_effect_runtime *rt, val_t int_val,
                        val_t *string_out) {
  *string_out = malloc(32);
  snprintf(*string_out, 31, "%lu", (intptr_t)int_val);
}

void unique_effect_concat(struct unique_effect_runtime *rt, val_t a, val_t b,
                          val_t *result) {
  size_t la = strlen(a), lb = strlen(b);
  char *buf = malloc(la + lb + 1);
  memcpy(&buf[0], a, la);
  memcpy(&buf[la], b, lb);
  buf[la + lb] = '\0';
  *result = buf;
}

void unique_effect_exit(struct unique_effect_runtime *rt, void *state) {
  // All timers must have been fired or cancelled.
  for (int i = 0; i < rt->next_timer; i++) {
    assert(rt->timers[i] == NULL);
  }

  assert(rt->next_call == rt->current_call + 1);
  rt->called_exit = true;
}

void unique_effect_len(struct unique_effect_runtime *rt, val_t message,
                       val_t *result) {
  *result = (void *)(intptr_t)strlen((char *)message);
}

void unique_effect_fork(struct unique_effect_runtime *rt, val_t parent,
                        val_t *a_out, val_t *b_out) {
  assert(parent == kSingletonClock);
  *a_out = parent;
  *b_out = parent;
}

void unique_effect_join(struct unique_effect_runtime *rt, val_t a, val_t b,
                        val_t *result) {
  assert(a == kSingletonClock);
  assert(b == kSingletonClock);
  *result = a;
}

void unique_effect_first(struct unique_effect_runtime *runtime,
                         struct unique_effect_first_state *state) {
  if (state->r[0].ready && !state->r[1].ready) {
    cancel_sleep(runtime, &state->r[1]);
  } else if (!state->r[0].ready && state->r[1].ready) {
    cancel_sleep(runtime, &state->r[0]);
  }

  if (!state->r[0].ready || !state->r[1].ready) {
    return;
  }

  assert(state->r[0].value == kSingletonClock);
  assert(state->r[1].value == kSingletonClock);

  state->result[0]->value = kSingletonClock;
  state->result[0]->ready = true;

  unique_effect_runtime_schedule(runtime, state->caller);
  free(state);
}

void unique_effect_copy(struct unique_effect_runtime *rt, val_t a,
                        val_t *result) {
  *result = strdup(a);
}

void unique_effect_append(struct unique_effect_runtime *rt,
                          struct unique_effect_array *ary, val_t value,
                          struct unique_effect_array **ary_out) {
  if (ary->length == ary->capacity) {
    ary = realloc(ary, sizeof(struct unique_effect_array) +
                           sizeof(val_t) * ary->capacity * 2 + 1);
    ary->capacity = 2 * ary->capacity + 1;
  }
  ary->elements[ary->length++] = value;
  *ary_out = ary;
}

void unique_effect_debug(struct unique_effect_runtime *rt,
                         struct unique_effect_array *ary, val_t *result_out) {
  char *result = malloc(512);
  result[0] = '[';
  int length = 1;
  for (int i = 0; i < ary->length; i++) {
    length += snprintf(&result[length], 512 - length, i == 0 ? "%ld" : ", %ld",
                       (intptr_t)ary->elements[i]);
  }
  if (length < 512)
    result[length++] = ']';
  if (length < 512)
    result[length++] = '\0';
  *result_out = result;
}

void unique_effect_mightfail(struct unique_effect_runtime *rt, val_t fs,
                             val_t *fs_out, val_t *result) {
  assert(fs == kSingletonFileSystem || fs == kSingletonFileSystemWillFail);

  *fs_out = fs == kSingletonFileSystem ? kSingletonFileSystemWillFail
                                       : kSingletonFileSystem;
  *result = malloc(sizeof(val_t) * 2);

  if (fs == kSingletonFileSystem) {
    ((val_t *)*result)[0] = (val_t)(intptr_t)0;
    ((val_t *)*result)[1] = strdup("Success!");
  } else {
    ((val_t *)*result)[0] = (val_t)(intptr_t)1;
    ((val_t *)*result)[1] = NULL;
  }
}

void unique_effect_reason(struct unique_effect_runtime *rt, val_t err,
                          val_t *reason) {
  *reason = strdup("some error");
}

void unique_effect_runtime_init(struct unique_effect_runtime *rt) {
  rt->next_call = 0;
  rt->next_timer = 0;
  rt->current_call = 0;
  rt->current_time = 0.0;
}

void unique_effect_runtime_loop(struct unique_effect_runtime *runtime) {
#ifdef USE_LIBUV
  finish_current_iteration(runtime);

  uv_run(uv_default_loop(), UV_RUN_DEFAULT);
  uv_loop_close(uv_default_loop());
#else
  while (true) {
    finish_current_iteration(runtime);
    if (runtime->next_timer > 0) {
      bool any = false;
      for (int i = 0; i < runtime->next_timer; i++) {
        if (runtime->timers[i] == NULL) {
          // timer has been cancelled
          continue;
        }
        any = true;
        unique_effect_runtime_schedule(runtime, runtime->timers[i]->caller);
        runtime->timers[i]->result[0]->value = kSingletonClock;
        runtime->timers[i]->result[0]->ready = true;
        free(runtime->timers[i]);
      }
      runtime->next_timer = 0;
      if (any) {
        runtime->current_time += 1.0;
      }
    } else {
      break;
    }
  }
#endif

  printf("finished after %0.1fs\n", runtime->current_time);
  assert(runtime->called_exit);
}
