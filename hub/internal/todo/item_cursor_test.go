package todo

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCursorRoundTrip(t *testing.T) {
	due := time.Date(2026, 11, 13, 22, 59, 59, 0, time.UTC)
	rank := 2
	in := itemCursor{Sort: "due", Order: "desc", ID: uuid.Must(uuid.NewV7()), Due: &due, Rank: &rank}
	out, err := decodeItemCursor(in.encode(), "due", "desc")
	require.NoError(t, err)
	require.Equal(t, in.ID, out.ID)
	require.True(t, due.Equal(*out.Due))
	require.Equal(t, 2, *out.Rank)

	_, err = decodeItemCursor(in.encode(), "due", "asc")
	require.Error(t, err)
	_, err = decodeItemCursor(in.encode(), "created", "desc")
	require.Error(t, err)
	_, err = decodeItemCursor("not base64 json", "due", "desc")
	require.Error(t, err)
}
