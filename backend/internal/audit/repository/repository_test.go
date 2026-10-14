package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// insertActivity menulis satu baris activities langsung lewat role runtime (app_user), dengan
// created_at eksplisit supaya urutan hasil deterministik.
func insertActivity(t *testing.T, db *testpg.DB, requestID string, org *uuid.UUID, action string, at time.Time) {
	t.Helper()
	_, err := db.App.Exec(context.Background(),
		`INSERT INTO activities (organization_id, action, result, request_id, created_at)
		 VALUES ($1, $2, 'success', $3, $4)`, org, action, requestID, at)
	require.NoError(t, err)
}

func TestListActivitiesByRequestID(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t)
	q := repository.New(db.App)

	orgA, orgB := id.NewID(), id.NewID()
	base := time.Now().UTC().Truncate(time.Microsecond)
	insertActivity(t, db, "req-1", &orgA, "a.first", base)
	insertActivity(t, db, "req-1", &orgB, "b.second", base.Add(time.Second))
	insertActivity(t, db, "req-1", nil, "platform.third", base.Add(2*time.Second))
	insertActivity(t, db, "req-2", &orgA, "other.request", base.Add(3*time.Second))

	t.Run("tanpa filter organisasi: semua baris request itu, urut created_at", func(t *testing.T) {
		got, err := q.ListActivitiesByRequestID(ctx, repository.ListActivitiesByRequestIDParams{RequestID: "req-1"})
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, []string{"a.first", "b.second", "platform.third"},
			[]string{got[0].Action, got[1].Action, got[2].Action})
	})

	t.Run("dengan organisasi: hanya milik organisasi itu", func(t *testing.T) {
		got, err := q.ListActivitiesByRequestID(ctx, repository.ListActivitiesByRequestIDParams{
			RequestID: "req-1", OrganizationID: &orgA,
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "a.first", got[0].Action)
		require.NotNil(t, got[0].OrganizationID)
		require.Equal(t, orgA, *got[0].OrganizationID)
	})

	t.Run("kolom nullable kembali sebagai pointer nil, bukan nol palsu", func(t *testing.T) {
		got, err := q.ListActivitiesByRequestID(ctx, repository.ListActivitiesByRequestIDParams{RequestID: "req-2"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		a := got[0]
		require.Nil(t, a.ActorUserID)
		require.Nil(t, a.ResourceType)
		require.Nil(t, a.IpAddress)
		require.Nil(t, a.TraceID)
		require.NotNil(t, a.RequestID)
		require.Equal(t, "req-2", *a.RequestID)
		require.JSONEq(t, `{}`, string(a.Metadata))
		require.WithinDuration(t, base.Add(3*time.Second), a.CreatedAt, time.Millisecond)
	})

	t.Run("request tak dikenal: slice kosong (bukan nil)", func(t *testing.T) {
		got, err := q.ListActivitiesByRequestID(ctx, repository.ListActivitiesByRequestIDParams{RequestID: "tidak-ada"})
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}
