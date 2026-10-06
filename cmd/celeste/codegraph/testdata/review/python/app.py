class Index:
    def __init__(self):
        self.items = []

    def add(self, x):
        self.items.append(x)

    def search(self, q):
        # TODO: real search
        pass


def tokenize(s):
    return s.split()


def is_ready():
    return True


def never_used():
    pass


def not_done():
    raise NotImplementedError("later")


def run():
    url = "http://localhost:9000/api"
    idx = Index()
    for t in tokenize(url):
        idx.add(t)


@app.route("/x")
def handler():
    # TODO: render
    pass


class Cfg:
    @staticmethod
    def unused_static():
        pass
