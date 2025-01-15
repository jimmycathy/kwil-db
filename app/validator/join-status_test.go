package validator

import (
	"encoding/json"
	"testing"

	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/types"
	"github.com/stretchr/testify/require"
)

func Test_respValJoinStatus_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		response respValJoinStatus
		want     string
	}{
		{
			name: "basic marshal",
			response: respValJoinStatus{
				Data: &types.JoinRequest{
					Candidate: types.NodeKey{
						PubKey: []byte{0x12, 0x34},
						Type:   crypto.KeyTypeSecp256k1,
					},
					Power: 100,
					Board: []types.NodeKey{
						{PubKey: []byte{0xEF, 0x12}},
					},
					Approved: []bool{true, false},
				},
			},
			want: `{"candidate":{"pubkey":"1234","type":0},"power":100,"board":[{"pubkey":"ef12","type":0}],"approved":[true,false]}`,
		},
		{
			name: "empty board",
			response: respValJoinStatus{
				Data: &types.JoinRequest{
					Candidate: types.NodeKey{
						PubKey: []byte{0xFF},
						Type:   crypto.KeyTypeSecp256k1,
					},
					Power:    50,
					Board:    []types.NodeKey{},
					Approved: []bool{},
				},
			},
			want: `{"candidate":{"pubkey":"ff","type":0},"power":50,"board":[],"approved":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(&tt.response)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(got))
		})
	}
}

func Test_respValJoinStatus_MarshalText(t *testing.T) {
	tests := []struct {
		name     string
		response respValJoinStatus
		want     string
	}{
		{
			name: "all approvals",
			response: respValJoinStatus{
				Data: &types.JoinRequest{
					Candidate: types.NodeKey{PubKey: []byte{0x12, 0x34}, Type: crypto.KeyTypeSecp256k1},
					Power:     1000,
					ExpiresAt: 5000,
					Board: []types.NodeKey{
						{PubKey: []byte{0xAB, 0xCD}, Type: crypto.KeyTypeSecp256k1},
						{PubKey: []byte{0xEF, 0x12}, Type: crypto.KeyTypeSecp256k1},
						{PubKey: []byte{0x56, 0x78}, Type: crypto.KeyTypeSecp256k1},
					},
					Approved: []bool{true, true, true},
				},
			},
			want: "Candidate: NodeKey{pubkey = 1234, keyType = secp256k1}\nRequested Power: 1000\nExpiration Height: 5000\n3 Approvals Received (2 needed):\nValidator NodeKey{pubkey = abcd, keyType = secp256k1}, approved\nValidator NodeKey{pubkey = ef12, keyType = secp256k1}, approved\nValidator NodeKey{pubkey = 5678, keyType = secp256k1}, approved\n",
		},
		{
			name: "mixed approvals",
			response: respValJoinStatus{
				Data: &types.JoinRequest{
					Candidate: types.NodeKey{PubKey: []byte{0xFF}, Type: crypto.KeyTypeSecp256k1},
					Power:     500,
					ExpiresAt: 1000,
					Board: []types.NodeKey{
						{PubKey: []byte{0x11, 0x22}, Type: crypto.KeyTypeSecp256k1},
						{PubKey: []byte{0x33, 0x44}, Type: crypto.KeyTypeSecp256k1},
					},
					Approved: []bool{true, false},
				},
			},
			want: "Candidate: NodeKey{pubkey = ff, keyType = secp256k1}\nRequested Power: 500\nExpiration Height: 1000\n1 Approvals Received (2 needed):\nValidator NodeKey{pubkey = 1122, keyType = secp256k1}, approved\nValidator NodeKey{pubkey = 3344, keyType = secp256k1}, not approved\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.response.MarshalText()
			require.NoError(t, err)
			require.Equal(t, tt.want, string(got))
		})
	}
}
