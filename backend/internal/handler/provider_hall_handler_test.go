package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProviderHallGroupIDs(t *testing.T) {
	first := int64(11)
	second := int64(22)

	ids := providerHallGroupIDs([]*service.ProviderHallView{
		{GroupID: &first},
		{GroupID: nil},
		{GroupID: &first},
		nil,
		{GroupID: &second},
	})

	require.Equal(t, []int64{first, second}, ids)
	require.Empty(t, providerHallGroupIDs(nil))
}
