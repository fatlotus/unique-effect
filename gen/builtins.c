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
static void sleep_adapter_result(uv_timer_t *timer) {
  struct unique_effect_sleep_state *state = (struct unique_effect_sleep_state *)timer->data;
  struct unique_effect_runtime *runtime = state->runtime;

  runtime->current_time = state->trigger_time;

  state->result[0]->value = kSingletonClock;
  state->result[0]->ready = true;

  unique_effect_runtime_schedule(runtime, state->caller);

  free(state);

  finish_current_iteration(runtime);
}
#endif

void unique_effect_sleep(struct unique_effect_runtime *rt,
                         struct unique_effect_sleep_state *state) {
  if (state->result[0]->cancelled && !state->r[0].cancelled) {
    state->r[0].cancelled = true;

    // Cancel the pending timer.
#ifdef USE_LIBUV
    assert(uv_timer_stop(&state->timer) == 0);
#else
    *state->pending_timer = NULL;
#endif

    state->result[0]->value = kSingletonClock;
    state->result[0]->ready = true;

    unique_effect_runtime_schedule(rt, state->caller);
    free(state);
    return;
  }

  // Wait until the previous timer completes before starting this one.
  if (!state->r[0].ready || !state->r[1].ready) {
    return;
  }

  // Make sure that repeated calls are ignored, emulating a user function.
  if (state->conditions[0]) {
    return;
  }

  int duration_in_seconds = (uintptr_t)state->r[1].value;

  state->conditions[0] = true;
  state->trigger_time = rt->current_time + duration_in_seconds;

#ifdef USE_LIBUV
  state->timer.data = state;
  state->runtime = rt;

  uv_timer_init(uv_default_loop(), &state->timer);
  uv_timer_start(&state->timer, &sleep_adapter_result, duration_in_seconds * 100, 0);
#else
  assert(state->r[0].value == kSingletonClock);
  assert(rt->next_timer < 20);

  // Register the new timer in the runtime.
  rt->timers[rt->next_timer] = state;

  // Store an intermediate value in case we need to cancel this clock, and wake
  // up the caller to let them know about it.
  state->pending_timer = &rt->timers[rt->next_timer];
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
  if (state->r[0].ready && !state->r[1].ready && !state->r[1].cancelled) {
    state->r[1].cancelled = true;
    unique_effect_runtime_schedule(runtime, state->caller);
    return;
  } else if (state->r[1].ready && !state->r[0].ready && !state->r[0].cancelled) {
    state->r[0].cancelled = true;
    unique_effect_runtime_schedule(runtime, state->caller);
    return;
  }

  if (!state->r[0].ready || !state->r[1].ready) {
    return;
  }

  assert(state->r[0].value == kSingletonClock);
  assert(state->r[1].value == kSingletonClock);

  state->result[0]->value = kSingletonClock;
  state->result[0]->ready = true;

  state->result[1]->value = kSingletonClock;
  state->result[1]->ready = true;

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
      double next_trigger_time = -1;
      // Look for the next timer event.
      for (int i = 0; i < runtime->next_timer; i++) {
        if (runtime->timers[i] != NULL &&
            (next_trigger_time < 0 ||
             runtime->timers[i]->trigger_time < next_trigger_time)) {
          next_trigger_time = runtime->timers[i]->trigger_time;
        }
      }
      for (int i = 0; i < runtime->next_timer; i++) {
        if (runtime->timers[i] == NULL ||
            runtime->timers[i]->trigger_time > next_trigger_time) {
          // timer has been cancelled or hasn't fired yet
          continue;
        }
        unique_effect_runtime_schedule(runtime, runtime->timers[i]->caller);
        runtime->timers[i]->result[0]->value = kSingletonClock;
        runtime->timers[i]->result[0]->ready = true;
        free(runtime->timers[i]);
        runtime->timers[i] = NULL;
      }
      if (next_trigger_time >= 0) {
        runtime->current_time = next_trigger_time;
      } else {
        // There's nothing left to do, so free up all the completed slots
        // currently in the runtime.
        runtime->next_timer = 0;
      }
    } else {
      break;
    }
  }
#endif

  printf("finished after %0.1fs\n", runtime->current_time);
  assert(runtime->called_exit);
}
