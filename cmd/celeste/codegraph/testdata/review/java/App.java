class Greeter implements Speaker {
    Greeter() {}

    @Override
    public void speak() {
        System.out.println("hi");
    }

    void unusedMethod() {
    }

    void later() {
        // FIXME: wire this up
    }
}

class App {
    static void run(Speaker s) {
        s.speak();
    }

    public static void main(String[] args) {
        run(new Greeter());
    }
}
