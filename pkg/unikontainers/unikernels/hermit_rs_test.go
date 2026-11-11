package unikernels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHermitCommandString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		hermit   *Hermit
		expected string
	}{
		{
			name:     "no network configured",
			hermit:   &Hermit{},
			expected: "",
		},
		{
			name: "with network configured",
			hermit: &Hermit{
				Net: HermitNet{
					Address: "10.0.0.2",
					Mask:    24,
					Gateway: "10.0.0.1",
				},
			},
			expected: "ip=10.0.0.2/24 gateway=10.0.0.1",
		},
		{
			name: "with DNS configured",
			hermit: &Hermit{
				Net: HermitNet{
					Address:   "10.0.0.2",
					Mask:      24,
					Gateway:   "10.0.0.1",
					DNSServer: "1.1.1.1",
				},
			},
			expected: "ip=10.0.0.2/24 gateway=10.0.0.1 env=HERMIT_DNS1=1.1.1.1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := tc.hermit.CommandString()
			require.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
