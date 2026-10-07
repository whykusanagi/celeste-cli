export function main() {
  const s = new Stack();
  s.push(add(1, 2));
  return square(3);
}

function add(a: number, b: number): number { return a + b; }

function square(x: number) {
  return multiply(x, x);
}

function multiply(a: number, b: number) { return a * b; }

class Stack {
  items: number[] = [];

  constructor(private readonly limit: number = 10) {}

  push(x: number) {
    this.items.push(add(x, 0));
  }

  reset() {
    // TODO: clear the items
  }

  neverCalled() {}
}

export function apiEntry() {
  throw new Error("not implemented");
}
