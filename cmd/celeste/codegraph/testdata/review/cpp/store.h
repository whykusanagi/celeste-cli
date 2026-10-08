class Store {
public:
    void put();
    void inlinePub() {}
private:
    void evict();
    void inlinePriv() {}
};

struct Plain {
    void open() {}
};

class Hidden {
    void defaultPriv() {}
};
