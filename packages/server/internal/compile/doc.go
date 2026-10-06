// Package compile is the compile coordinator: it turns a space's record
// into the files and copy-outs its targets deliver (plan 25 §5.7).
//
// The worker runs it as two River jobs (worker.go): compile_target, which
// the ledger inserts in the transaction of every command that changes
// what compiles, and a 30 s sweeper that re-enqueues any target whose
// dirty generation is ahead of its compiled one. Service.Run builds the
// compiler input from one ledger snapshot (input.go), calls the stateless
// compile service (packages/compile-service) over HTTP (client.go),
// stores the outputs in object storage (artifact.go) and records the run
// through the ledger as Memax. Nothing here writes the record directly:
// every write is a ledger command, with its receipt.
//
// The API uses the same Service for what needs the outputs' words: the
// preview, observations of hand edits (the compiler's parseBack) and the
// drift view.
//
// Package compiletest has an in-process Fake compiler and StartService,
// which builds and runs the real Node service for tests.
package compile
