interface Closer {
  close(): void;
}

class Pipe implements Closer {
  close() {
    // TODO: release the buffers
  }
}

const ready = () => true;
