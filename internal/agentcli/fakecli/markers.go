package fakecli

// Markers are the words of hidane's charters and turn prompts by which the
// fake CLIs (cmd/fakeagent) tell which role is asking and what happened.
// internal/agents/fakemarkers_test.go asserts each one still appears in what
// hidane sends, so a reworded prompt fails there by name instead of leaving
// the fake answering as if nothing had happened.
const (
	// In a role's charter, its system prompt.
	PrimaryRole   = "Primary agent of hidane"
	ManagerRole   = "Manager of one work item"
	WorkerRole    = "Worker execution"
	DistillerRole = "memory distiller"
	PingRole      = "connectivity check"

	// In a Primary's and a Manager's turn.
	MessagesThisTurn = "Messages this turn:"
	// The Primary's second try after an answer that was not its effect list.
	PrimaryRetry = "was not the JSON effect list"
	// The Manager's second try after an answer without an action.
	ManagerNudge = "Your answer contained no action"
	// ItemTitle formats with a work item's title as its Manager is told it.
	ItemTitle       = " — %s (status: "
	ChildItem       = "You are a CHILD work item"
	ChildrenSettled = "(all child work items finished)"

	// A worker's outcome in its Manager's turn: the header and its status,
	// then the lines that go with it.
	WorkerResult    = "(worker result"
	ResultOK        = ": ok)"
	ResultBlocked   = ": blocked)"
	ResultCancelled = ": cancelled)"
	BlockedOn       = "blocked on: "
	ResultError     = "error: "
	ResultSummary   = "summary:\n"
	// What the person said during the run, given to the worker or too late.
	SteerGiven = "the result above should account for it:\n- "
	SteerLate  = "the worker never saw it:\n- "

	// In the distiller's prompt.
	ExistingMemories = "Existing memories"
	RecentEvents     = "Recent events:"
)
