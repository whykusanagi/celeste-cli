namespace geo {
int mul(int a, int b) { return a * b; }
int area(int w, int h) { return mul(w, h); }
}

class Shape {
public:
    Shape() {}
    virtual int sides() = 0;
};

void unusedCpp() {
}

int runShapes() {
    return geo::area(2, 3);
}

class Circle : public Shape {
public:
    int sides() override {
        // TODO: count them
    }
};
