package failpoint

// Name identifies an explicit protocol boundary from Integration-tests.md.
type Name string

const (
	// OperationAdmission is the operation admission boundary.
	OperationAdmission Name = "operation.admission"
	// JournalAppend is the journal append boundary.
	JournalAppend Name = "journal.append"
	// LocalSyncCompletion is the local journal sync completion boundary.
	LocalSyncCompletion Name = "local.sync_completion"
	// PartnerReceipt is the synchronous partner receipt boundary.
	PartnerReceipt Name = "partner.receipt"
	// PartnerACKTransmit is the synchronous partner ACK transmission boundary.
	PartnerACKTransmit Name = "partner.ack_transmit"
	// CommitVisibilityDecision is the commit/visibility decision boundary.
	CommitVisibilityDecision Name = "commit.visibility_decision"
	// Apply is the operation application boundary.
	Apply Name = "apply"
	// ClientReply is the client reply boundary.
	ClientReply Name = "client.reply"
	// EpochPersistence is the epoch persistence boundary.
	EpochPersistence Name = "epoch.persistence"
	// CheckpointPublication is the checkpoint publication boundary.
	CheckpointPublication Name = "checkpoint.publication"
	// JournalTrim is the journal trimming boundary.
	JournalTrim Name = "journal.trim"
)

// StandardNames returns a fresh list of registered names in protocol order.
// Hooks must document their precise before/after placement when instrumented.
func StandardNames() []Name {
	return []Name{
		OperationAdmission, JournalAppend, LocalSyncCompletion, PartnerReceipt,
		PartnerACKTransmit, CommitVisibilityDecision, Apply, ClientReply,
		EpochPersistence, CheckpointPublication, JournalTrim,
	}
}

func known(name Name) bool {
	switch name {
	case OperationAdmission, JournalAppend, LocalSyncCompletion, PartnerReceipt,
		PartnerACKTransmit, CommitVisibilityDecision, Apply, ClientReply,
		EpochPersistence, CheckpointPublication, JournalTrim:
		return true
	default:
		return false
	}
}
