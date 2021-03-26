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

struct unique_effect_runtime {
  closure_t upcoming_calls[100];
  int next_call;
  int current_call;

  // Compatibility mode (in case libuv is unavailable).
  struct unique_effect_sleep_state *timers[20];
  int next_timer;

  bool called_exit;
  double current_time;
};

struct unique_effect_sleep_state {
  future_t r[1];
  future_t *result[1];
  closure_t caller;
  bool conditions[1]; // needed for calling convention
};

struct unique_effect_first_state {
  future_t r[2];
  future_t *result[1];
  closure_t caller;
  bool conditions[1]; // needed for calling convention
};

struct unique_effect_array {
  int length;
  int capacity;
  val_t elements[];
};

extern val_t kSingletonConsole;
extern val_t kSingletonClock;
extern val_t kSingletonFileSystem;

void unique_effect_runtime_init(struct unique_effect_runtime *rt);
void unique_effect_runtime_schedule(struct unique_effect_runtime *rt,
                                    closure_t closure);
void unique_effect_runtime_loop(struct unique_effect_runtime *rt);
void unique_effect_exit(struct unique_effect_runtime *rt, void *state);

#endif
