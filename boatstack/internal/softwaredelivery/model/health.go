package model

// OperationalHealth is a read-only projection of canonical facts used by
// diagnostics. It cannot admit or execute a transition.
type OperationalHealth struct {
	RuntimeVerified  bool
	RecoveryRequired bool
}

func ProjectOperationalHealth(snapshot Snapshot) OperationalHealth {
	return OperationalHealth{
		RuntimeVerified:  snapshot.Runtime.Status == FactKnown && snapshot.Runtime.Value == RuntimeVerified,
		RecoveryRequired: snapshot.Phase.Value == PhaseRecovery || snapshot.Recovery.Value != RecoveryNone || snapshot.Transaction.Value != TransactionNone,
	}
}
