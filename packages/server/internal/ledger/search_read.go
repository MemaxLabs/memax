package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MemoryWithReceipts is a memory as a person's search shows it (GET
// /v2/search): its row, without sources, links or the judge's verdict, and
// the receipts its row names, the first and the latest (one when they are
// the same), which say who wrote it and who changed it last. Receipts
// never hold words.
type MemoryWithReceipts struct {
	Memory   Memory
	Receipts []Receipt
}

// MemoriesWithReceipts reads the memories ids names that are in the
// scope's spaces, each with the receipts its row names, in one round trip
// (ReadBatch: both statements and COMMIT together). The order is the
// caller's to set: a memory that is gone (forgotten words still leave its
// row; a space left the scope) is simply missing from the map.
func (l *Ledger) MemoriesWithReceipts(ctx context.Context, scope Scope, ids []uuid.UUID) (map[uuid.UUID]MemoryWithReceipts, error) {
	out := map[uuid.UUID]MemoryWithReceipts{}
	if l == nil {
		return nil, ErrDisabled
	}
	if len(ids) == 0 {
		return out, nil
	}
	spaces := scope.SpaceIDs()
	var memories []*Memory
	var receipts []Receipt
	b := &pgx.Batch{}
	b.Queue(memorySelect+` WHERE m.id = ANY($1) AND m.space_id = ANY($2)`, ids, spaces).
		Query(func(rows pgx.Rows) error {
			var err error
			memories, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Memory, error) { return scanMemory(r) })
			if err != nil {
				return fmt.Errorf("ledger: read memories: %w", err)
			}
			return nil
		})
	// The receipts the rows name, found through the rows themselves, so
	// they go in the same round trip.
	b.Queue(receiptSelect+`
		 WHERE space_id = ANY($2) AND id IN (
		       SELECT created_receipt_id FROM v2.memories WHERE id = ANY($1) AND space_id = ANY($2)
		       UNION
		       SELECT last_receipt_id FROM v2.memories WHERE id = ANY($1) AND space_id = ANY($2))`, ids, spaces).
		Query(func(rows pgx.Rows) error {
			var err error
			receipts, err = pgx.CollectRows(rows, scanReceipt)
			if err != nil {
				return fmt.Errorf("ledger: read receipts: %w", err)
			}
			return nil
		})
	if err := l.ReadBatch(ctx, scope, b); err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]Receipt, len(receipts))
	for _, rc := range receipts {
		byID[rc.ID] = rc
	}
	for _, m := range memories {
		hit := MemoryWithReceipts{Memory: *m}
		if rc, ok := byID[m.CreatedReceiptID]; ok {
			hit.Receipts = append(hit.Receipts, rc)
		}
		if m.LastReceiptID != m.CreatedReceiptID {
			if rc, ok := byID[m.LastReceiptID]; ok {
				hit.Receipts = append(hit.Receipts, rc)
			}
		}
		out[m.ID] = hit
	}
	return out, nil
}
