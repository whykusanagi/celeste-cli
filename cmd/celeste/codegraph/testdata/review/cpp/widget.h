class Widget {
public:
    virtual void draw() = 0;
};

class Button : public Widget {
public:
    void draw() override;
    void paintEvent(Event *e) override;
};
