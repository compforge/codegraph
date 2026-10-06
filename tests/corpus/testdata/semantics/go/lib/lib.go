package lib

type worker struct{}

func (worker) Work() {}

type server struct{ service worker }

func (s *server) Create() { s.service.Work() }

type Box struct{ worker }

func Work() {}
