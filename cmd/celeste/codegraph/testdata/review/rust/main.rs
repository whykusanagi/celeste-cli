trait Speak {
    fn speak(&self);
}

struct Dog;

impl Speak for Dog {
    fn speak(&self) {}
}

impl Drop for Dog {
    fn drop(&mut self) {}
}

fn dead_rs() {}

fn main() {
    let _d = Dog;
}
