class Base {
public:
    virtual void f() = 0;
};

class D : public Base {
public:
    void f() override;
    void g();
};
