#include <assert.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "builtins.h"
#include GENERATED_MODULE_HEADER

static void *kSingletonConsole = (void *)40;
static void *kSingletonClock = (void *)50;

void hang10_runtime_schedule(struct hang10_runtime *rt, closure_t closure) {
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

void hang10_print(val_t console, val_t msg, val_t *console_out) {
  assert(console == kSingletonConsole);
  printf("%s\n", (char *)msg);
  *console_out = console;
}

void hang10_sleep(struct hang10_runtime *rt, struct hang10_sleep_state *state) {
  assert(rt->next_delay < 20);
  assert(state->r[0].value == kSingletonClock);

  *state->result[0] = state->r[0];

  rt->after_delay[rt->next_delay++] = state->caller;
  free(state);
}

void hang10_ReadLine(val_t console, val_t *console_out, val_t *name_out) {
  assert(console == kSingletonConsole);
  *name_out = "World";
  *console_out = console;
}

void hang10_itoa(val_t int_val, val_t *string_out) {
  *string_out = malloc(32);
  snprintf(*string_out, 31, "%lu", (uintptr_t)int_val);
}

void hang10_concat(val_t a, val_t b, val_t *result) {
  size_t la = strlen(a), lb = strlen(b);
  char *buf = malloc(la + lb + 1);
  memcpy(&buf[0], a, la);
  memcpy(&buf[la], b, lb);
  buf[la + lb] = '\0';
  *result = buf;
}

static void hang10_exit(struct hang10_runtime *rt, void *state) { exit(0); }

void hang10_isShort(val_t message, val_t *result) {
  *result = strlen((char *)message) < 40 ? (void *)true : (void *)false;
}

void hang10_fork(val_t parent, val_t *a_out, val_t *b_out) {
  assert(parent == kSingletonClock);
  *a_out = parent;
  *b_out = parent;
}

void hang10_join(val_t a, val_t b, val_t *result) {
  assert(a == kSingletonClock);
  assert(b == kSingletonClock);
  *result = a;
}

int main(int argc, const char *argv[]) {
  struct hang10_runtime rt;
  rt.next_call = 0;
  rt.next_delay = 0;

  struct hang10_main_state *st = malloc(sizeof(struct hang10_main_state));
  st->r[0].value = kSingletonClock;
  st->r[0].ready = true;
  st->r[1].value = kSingletonConsole;
  st->r[1].ready = true;
  st->result[0] = &st->r[0];
  st->result[1] = &st->r[1];
  st->caller = (closure_t){.state = NULL, .func = &hang10_exit};

  hang10_runtime_schedule(&rt, (closure_t){.state = st, .func = &hang10_main});

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
        hang10_runtime_schedule(&rt, rt.after_delay[i]);
      }
      rt.next_delay = 0;
    } else {
      break;
    }
  }

  fprintf(stderr, "** finished without calling exit **\n");
  return 1;
}