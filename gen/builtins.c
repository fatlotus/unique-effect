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

#include "builtins.h"
#include GENERATED_MODULE_HEADER

static void *kSingletonConsole = (void *)40;
static void *kSingletonClock = (void *)50;

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

void unique_effect_print(val_t console, val_t msg, val_t *console_out) {
  assert(console == kSingletonConsole);
  printf("%s\n", (char *)msg);
  *console_out = console;
}

void unique_effect_sleep(struct unique_effect_runtime *rt,
                         struct unique_effect_sleep_state *state) {
  assert(rt->next_delay < 20);
  assert(state->r[0].value == kSingletonClock);

  // We set ->ready = true after the sleep finishes.
  state->result[0]->value = kSingletonClock;

  rt->after_delay[rt->next_delay] = state->caller;
  rt->after_delay_futures[rt->next_delay++] = state->result[0];
  free(state);
}

void unique_effect_ReadLine(val_t console, val_t *console_out,
                            val_t *name_out) {
  assert(console == kSingletonConsole);
  *name_out = strdup("World");
  *console_out = console;
}

void unique_effect_itoa(val_t int_val, val_t *string_out) {
  *string_out = malloc(32);
  snprintf(*string_out, 31, "%lu", (intptr_t)int_val);
}

void unique_effect_concat(val_t a, val_t b, val_t *result) {
  size_t la = strlen(a), lb = strlen(b);
  char *buf = malloc(la + lb + 1);
  memcpy(&buf[0], a, la);
  memcpy(&buf[la], b, lb);
  buf[la + lb] = '\0';
  *result = buf;
}

static void unique_effect_exit(struct unique_effect_runtime *rt, void *state) {
  assert(rt->next_delay == 0);
  assert(rt->next_call == rt->current_call + 1);
  rt->called_exit = true;
}

void unique_effect_len(val_t message, val_t* result) {
    *result = (void*)(intptr_t)strlen((char *)message);
}

void unique_effect_fork(val_t parent, val_t *a_out, val_t *b_out) {
  assert(parent == kSingletonClock);
  *a_out = parent;
  *b_out = parent;
}

void unique_effect_join(val_t a, val_t b, val_t *result) {
  assert(a == kSingletonClock);
  assert(b == kSingletonClock);
  *result = a;
}

void unique_effect_copy(val_t a, val_t *result) { *result = strdup(a); }

int main(int argc, const char *argv[]) {
  struct unique_effect_runtime rt;
  rt.next_call = 0;
  rt.next_delay = 0;

  struct unique_effect_main_state *st =
      malloc(sizeof(struct unique_effect_main_state));
  st->r[0].value = kSingletonClock;
  st->r[0].ready = true;
  st->r[1].value = kSingletonConsole;
  st->r[1].ready = true;
  st->result[0] = &st->r[0];
  st->result[1] = &st->r[1];
  st->caller = (closure_t){.state = NULL, .func = &unique_effect_exit};

  unique_effect_runtime_schedule(
      &rt, (closure_t){.state = st, .func = &unique_effect_main});

  int i = 0;
  while (true) {
    for (; i < rt.next_call; i++) {
      // printf("-- thunk %d --\n", i);
      rt.current_call = i;
      rt.upcoming_calls[i].func(&rt, rt.upcoming_calls[i].state);
    }
    if (rt.next_delay > 0) {
      fprintf(stdout, "sleeping (%d outstanding timer[s])...", rt.next_delay);
      fflush(stdout);
      usleep(100000);
      fprintf(stdout, "done\n");
      for (int i = 0; i < rt.next_delay; i++) {
        unique_effect_runtime_schedule(&rt, rt.after_delay[i]);
        rt.after_delay_futures[i]->ready = true;
      }
      rt.next_delay = 0;
    } else {
      break;
    }
  }

  assert(rt.called_exit);
  return 0;
}