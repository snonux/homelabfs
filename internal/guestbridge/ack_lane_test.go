package guestbridge

import (
	"encoding/json"
	"testing"
)

// VIEW_ACK runs on its own worker lane (d84) so that a guest observation in
// its preparation cannot hold up the other control cells, while every other
// control cell keeps its lane. The lane picks only the worker: VIEW_ACK still
// spends a control credit (controlCommand). The controlled runtime's
// TestQueryAnsweredWhileACKObservationInFlight shows the effect end to end.
func TestViewAckHasItsOwnControlLane(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	tr, err := NewTransport(f.s, transportBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if cap(tr.viewAckQueue) != 16 {
		t.Fatalf("VIEW_ACK queue capacity %d, want the 16 control credits", cap(tr.viewAckQueue))
	}
	drain, err := json.Marshal(Call{Intent: Intent{Opcode: "DRAIN_WRITE"}})
	if err != nil {
		t.Fatal(err)
	}
	lanes := []struct {
		command Command
		control bool
		want    chan job
	}{
		{Command{Op: "VIEW_ACK"}, true, tr.viewAckQueue},
		{Command{Op: "QUERY"}, true, tr.controlQueue},
		{Command{Op: "CANCEL"}, true, tr.controlQueue},
		{Command{Op: "VIEW_POLL"}, true, tr.controlQueue},
		{Command{Op: "VIEW_QUERY"}, true, tr.controlQueue},
		{Command{Op: "RETIRE"}, true, tr.controlQueue},
		{Command{Op: "CALL", Payload: drain}, true, tr.drainQueue},
		{Command{Op: "CALL"}, false, tr.ordinaryQueue},
		{Command{Op: "VIEW_ACQUIRE"}, false, tr.acquisitionQueue},
		{Command{Op: "RELEASE_CELL"}, true, tr.replacementQueue},
	}
	for _, lane := range lanes {
		if got := tr.commandQueue(lane.command, lane.control); got != lane.want {
			t.Errorf("%s routed to the wrong lane", lane.command.Op)
		}
	}
	if !controlCommand(Command{Op: "VIEW_ACK"}) {
		t.Fatal("VIEW_ACK no longer spends a control credit")
	}
}
