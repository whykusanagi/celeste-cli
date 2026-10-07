#include <stdio.h>

int add(int a, int b) { return a + b; }

int twice(int x) {
    return add(x, x);
}

void todo_c(void) {
    /* TODO: finish this */
}

void dead_c(void) {
}

int main(void) {
    printf("%d\n", twice(add(1, 2)));
    return 0;
}
