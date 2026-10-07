package store

// Saver saves.
type Saver interface {
	Save()
}

// Disk is a Saver.
type Disk struct{}

// Save writes nothing yet, on purpose.
func (d *Disk) Save() {}

// Export is part of the package API and is not written yet.
func Export() {
	panic("not implemented")
}

func unexportedDead() {}
