package app

import (
	peer "example.org/peer/lib"
	child "example.org/root/child/lib"
	local "example.org/root/lib"
)

func Entry()  { local.Work(); child.Work(); peer.Work() }
func Shadow() { child := local.Box{}; child.Work() }
