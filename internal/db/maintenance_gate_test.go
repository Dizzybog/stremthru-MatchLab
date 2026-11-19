package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceWriterGateBlocksWritesAndTransactions(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", "file:maintenance_writer_gate_test?mode=memory&cache=shared")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	SetForTesting(sqlDB, t.Cleanup)

	_, err = Exec("CREATE TABLE maintenance_gate_test (id INTEGER NOT NULL)")
	require.NoError(t, err)

	maintenance, err := BeginMaintenance()
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = maintenance.Rollback()
		}
	})

	writeDone := make(chan error, 1)
	go func() {
		_, e := Exec("INSERT INTO maintenance_gate_test (id) VALUES (1)")
		writeDone <- e
	}()
	txDone := make(chan error, 1)
	go func() {
		tx, e := Begin()
		if e == nil {
			e = tx.Rollback()
		}
		txDone <- e
	}()

	select {
	case err := <-writeDone:
		t.Fatalf("ordinary write bypassed maintenance gate: %v", err)
	case <-time.After(70 * time.Millisecond):
	}
	select {
	case err := <-txDone:
		t.Fatalf("new transaction bypassed maintenance gate: %v", err)
	case <-time.After(70 * time.Millisecond):
	}

	// Maintenance status writes are deliberately allowed while paused.
	_, err = ExecMaintenanceStatus("INSERT INTO maintenance_gate_test (id) VALUES (2)")
	require.NoError(t, err)

	require.NoError(t, maintenance.Commit())
	closed = true

	select {
	case err := <-writeDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not resume after maintenance commit")
	}
	select {
	case err := <-txDone:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("transaction did not resume after maintenance commit")
	}

	var count int
	require.NoError(t, QueryRow("SELECT COUNT(*) FROM maintenance_gate_test").Scan(&count))
	require.Equal(t, 2, count)
}

func TestMaintenanceWaitsForActiveTransactionAndReleasesOnRollback(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", "file:maintenance_tx_wait_test?mode=memory&cache=shared")
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	SetForTesting(sqlDB, t.Cleanup)

	activeTx, err := Begin()
	require.NoError(t, err)
	_, err = activeTx.Exec("CREATE TABLE maintenance_tx_wait_test (id INTEGER NOT NULL)")
	require.NoError(t, err)

	maintenanceReady := make(chan *Tx, 1)
	go func() {
		tx, err := BeginMaintenance()
		if err == nil {
			maintenanceReady <- tx
		}
	}()
	select {
	case <-maintenanceReady:
		t.Fatal("maintenance started before active transaction drained")
	case <-time.After(70 * time.Millisecond):
	}

	require.NoError(t, activeTx.Commit())
	select {
	case maintenance := <-maintenanceReady:
		require.NoError(t, maintenance.Rollback())
	case <-time.After(3 * time.Second):
		t.Fatal("maintenance did not begin after transaction committed")
	}

	_, err = Exec("INSERT INTO maintenance_tx_wait_test (id) VALUES (1)")
	require.NoError(t, err)
}
