// Package ir owns the source-control analysis used by Asyncify lowering.
//
// Prepare decodes a private source tree, resolves control labels and block
// signatures, and classifies suspension through the shared call semantics.
// Analysis hides the tree and its facts; a source rewrite requires a new plan.
//
// Linearize consumes that plan to spill block/loop/if values and introduce
// rewind routing. Referenced function exits use the same result-frame contract
// as block exits. Branch depths are derived from bound source labels. The output
// owns its instruction immediates and carries checked source-call identities and
// suspension positions to the engine. The engine supplies the final function end.
//
// This is a control and suspension boundary, not a complete typed value IR.
// Operand liveness and storage assignment still follow control linearization;
// moving them before routing remains a separate pipeline change.
package ir
