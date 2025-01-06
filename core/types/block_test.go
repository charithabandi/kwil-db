package types

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/crypto/auth"

	"github.com/stretchr/testify/require"
)

func newTx(nonce uint64, sender, payload string) *Transaction {
	return &Transaction{
		Signature: &auth.Signature{},
		Body: &TransactionBody{
			Description: "test",
			Payload:     []byte(payload),
			Fee:         big.NewInt(0),
			Nonce:       nonce,
		},
		Sender: []byte(sender),
	}
}

func TestGetRawBlockTx(t *testing.T) {
	privKey, pubKey, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)

	makeRawBlock := func(txnPayloads [][]byte) ([]byte, *Block) {
		txns := make([]*Transaction, len(txnPayloads))
		for i, pl := range txnPayloads {
			txns[i] = newTx(uint64(i), "bob", string(pl))
		}
		blk := NewBlock(1, Hash{1, 2, 3}, Hash{6, 7, 8}, Hash{}, time.Unix(1729890593, 0), txns)
		err := blk.Sign(privKey)
		require.NoError(t, err)
		return EncodeBlock(blk), blk
	}

	t.Run("valid block signature", func(t *testing.T) {
		txns := [][]byte{[]byte("tx1")}
		rawBlock, _ := makeRawBlock(txns)
		blk, err := DecodeBlock(rawBlock)
		require.NoError(t, err)

		valid, err := blk.VerifySignature(pubKey)
		require.NoError(t, err)
		require.True(t, valid)
	})

	t.Run("valid transaction index", func(t *testing.T) {
		txns := [][]byte{
			[]byte("tx1"),
			[]byte("transaction2"),
			[]byte("tx3"),
		}
		rawBlock, blk := makeRawBlock(txns)
		const idx = 1
		rawTx, err := GetRawBlockTx(rawBlock, idx)
		if err != nil {
			t.Fatal(err)
		}
		txOut1 := blk.Txns[idx]
		rawTxOut1, err := txOut1.MarshalBinary()
		require.NoError(t, err)
		if !bytes.Equal(rawTx, rawTxOut1) {
			t.Errorf("got tx %x, want %x", rawTx, rawTxOut1)
		}
	})

	t.Run("invalid transaction length", func(t *testing.T) {
		buf := new(bytes.Buffer)
		header := &BlockHeader{Height: 1, NumTxns: 1}
		buf.Write(EncodeBlockHeader(header))
		binary.Write(buf, binary.LittleEndian, uint32(1<<30)) // Very large tx length

		_, err := GetRawBlockTx(buf.Bytes(), 0)
		if err == nil {
			t.Error("expected error for invalid transaction length")
		}
	})

	t.Run("index out of range", func(t *testing.T) {
		txns := [][]byte{[]byte("tx1")}
		rawBlock, _ := makeRawBlock(txns)

		_, err := GetRawBlockTx(rawBlock, 1)
		if err != ErrNotFound {
			t.Errorf("got err %v, want ErrNotFound", err)
		}
	})

	t.Run("corrupted block data", func(t *testing.T) {
		txns := [][]byte{[]byte("tx1")}
		rawBlock, _ := makeRawBlock(txns)
		blk, err := DecodeBlock(rawBlock)
		require.NoError(t, err)

		sigLen := len(blk.Signature) + 4
		corrupted := rawBlock[:len(rawBlock)-1-sigLen]

		_, err = GetRawBlockTx(corrupted, 0)
		if err == nil {
			t.Error("expected error for corrupted block data")
		}
	})

	t.Run("empty block", func(t *testing.T) {
		rawBlock, _ := makeRawBlock([][]byte{})

		_, err := GetRawBlockTx(rawBlock, 0)
		if err != ErrNotFound {
			t.Errorf("got err %v, want ErrNotFound", err)
		}
	})
}

func TestCalcMerkleRoot(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		root := CalcMerkleRoot([]Hash{})
		if root != (Hash{}) {
			t.Errorf("empty slice should return zero hash, got %x", root)
		}
	})

	t.Run("single leaf", func(t *testing.T) {
		leaf := Hash{1, 2, 3, 4}
		root := CalcMerkleRoot([]Hash{leaf})
		if root != leaf {
			t.Errorf("single leaf should return same hash, got %x, want %x", root, leaf)
		}
	})

	t.Run("two leaves", func(t *testing.T) {
		leaf1 := Hash{1, 2, 3, 4}
		leaf2 := Hash{5, 6, 7, 8}
		root := CalcMerkleRoot([]Hash{leaf1, leaf2})

		var buf [HashLen * 2]byte
		copy(buf[:HashLen], leaf1[:])
		copy(buf[HashLen:], leaf2[:])
		expected := HashBytes(buf[:])

		if root != expected {
			t.Errorf("got root %x, want %x", root, expected)
		}
	})

	t.Run("three leaves", func(t *testing.T) {
		leaves := []Hash{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
			{3, 3, 3, 3},
		}
		CalcMerkleRoot(leaves)

		// Verify original slice not modified
		if leaves[2] != (Hash{3, 3, 3, 3}) {
			t.Error("original slice was modified")
		}
	})

	t.Run("four leaves", func(t *testing.T) {
		leaves := []Hash{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
			{3, 3, 3, 3},
			{4, 4, 4, 4},
		}
		root1 := CalcMerkleRoot(leaves)

		// Calculate same root with modified order
		leaves[0], leaves[1] = leaves[1], leaves[0]
		root2 := CalcMerkleRoot(leaves)

		if root1 == root2 {
			t.Error("roots should differ when leaf order changes")
		}
	})

	t.Run("preserve input", func(t *testing.T) {
		original := []Hash{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
		}
		originalCopy := make([]Hash, len(original))
		copy(originalCopy, original)

		CalcMerkleRoot(original)

		for i := range original {
			if original[i] != originalCopy[i] {
				t.Errorf("input slice was modified at index %d", i)
			}
		}
	})
}

func TestBlock_EncodeDecode(t *testing.T) {
	t.Run("encode and decode empty block", func(t *testing.T) {
		original := &Block{
			Header: &BlockHeader{
				Version:          1,
				Height:           100,
				NumTxns:          0,
				PrevHash:         Hash{1, 2, 3},
				PrevAppHash:      Hash{4, 5, 6},
				ValidatorSetHash: Hash{7, 8, 9},
				Timestamp:        time.Now().UTC().Truncate(time.Millisecond),
				MerkleRoot:       Hash{10, 11, 12},
			},
			Txns:      []*Transaction{},
			Signature: []byte("test-signature"),
		}

		encoded := EncodeBlock(original)
		decoded, err := DecodeBlock(encoded)
		require.NoError(t, err)
		require.Equal(t, original.Header, decoded.Header)
		require.Equal(t, original.Signature, decoded.Signature)
		require.Empty(t, decoded.Txns)
	})

	t.Run("encode and decode block with multiple transactions", func(t *testing.T) {
		txns := []*Transaction{
			newTx(0, "bob", "tx1"),
			newTx(1, "bob", "transaction 2"),
			newTx(0, "alice", string(make([]byte, 1000))),
		}
		original := &Block{
			Header: &BlockHeader{
				Version:          1,
				Height:           100,
				NumTxns:          uint32(len(txns)),
				PrevHash:         Hash{1, 2, 3},
				PrevAppHash:      Hash{4, 5, 6},
				ValidatorSetHash: Hash{7, 8, 9},
				Timestamp:        time.Now().UTC().Truncate(time.Millisecond),
				MerkleRoot:       Hash{10, 11, 12},
			},
			Txns:      txns,
			Signature: []byte("test-signature-long"),
		}

		encoded := EncodeBlock(original)
		decoded, err := DecodeBlock(encoded)
		require.NoError(t, err)
		require.Equal(t, original.Header, decoded.Header)
		require.Equal(t, original.Signature, decoded.Signature)
		require.EqualExportedValues(t, original.Txns, decoded.Txns)
	})

	t.Run("decode with invalid signature length", func(t *testing.T) {
		buf := new(bytes.Buffer)
		header := &BlockHeader{Height: 1, NumTxns: 0}
		buf.Write(EncodeBlockHeader(header))
		binary.Write(buf, binary.LittleEndian, uint32(1<<31))

		_, err := DecodeBlock(buf.Bytes())
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid signature length")
	})

	t.Run("decode with invalid transaction length", func(t *testing.T) {
		buf := new(bytes.Buffer)
		header := &BlockHeader{Height: 1, NumTxns: 1}
		buf.Write(EncodeBlockHeader(header))
		// sigLen and sig
		binary.Write(buf, binary.LittleEndian, uint32(3))
		buf.Write([]byte("sig"))

		// first txLen -- too big
		binary.Write(buf, binary.LittleEndian, uint32(1<<31))

		_, err := DecodeBlock(buf.Bytes())
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid transaction length")
	})

	t.Run("decode with truncated data", func(t *testing.T) {
		original := &Block{
			Header: &BlockHeader{
				Version:   1,
				Height:    100,
				NumTxns:   1,
				Timestamp: time.Now(),
			},
			Txns:      []*Transaction{newTx(0, "bob", "a")},
			Signature: []byte("sig"),
		}
		encoded := EncodeBlock(original)
		truncated := encoded[:len(encoded)-10]

		_, err := DecodeBlock(truncated)
		require.Error(t, err)
	})
}

func TestCalcMerkleRoot_Extended(t *testing.T) {
	t.Run("five leaves", func(t *testing.T) {
		leaves := []Hash{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
			{3, 3, 3, 3},
			{4, 4, 4, 4},
			{5, 5, 5, 5},
		}
		root := CalcMerkleRoot(leaves)
		require.NotEqual(t, Hash{}, root)

		// Verify the tree structure by calculating intermediate nodes
		var buf [HashLen * 2]byte
		copy(buf[:HashLen], leaves[0][:])
		copy(buf[HashLen:], leaves[1][:])
		h01 := HashBytes(buf[:])

		copy(buf[:HashLen], leaves[2][:])
		copy(buf[HashLen:], leaves[3][:])
		h23 := HashBytes(buf[:])

		copy(buf[:HashLen], leaves[4][:])
		copy(buf[HashLen:], leaves[4][:])
		h44 := HashBytes(buf[:])

		copy(buf[:HashLen], h01[:])
		copy(buf[HashLen:], h23[:])
		h0123 := HashBytes(buf[:])

		copy(buf[:HashLen], h44[:])
		copy(buf[HashLen:], h44[:])
		h4444 := HashBytes(buf[:])

		copy(buf[:HashLen], h0123[:])
		copy(buf[HashLen:], h4444[:])
		expected := HashBytes(buf[:])

		require.Equal(t, expected, root)
	})

	t.Run("seven leaves", func(t *testing.T) {
		leaves := []Hash{
			{1, 1, 1, 1},
			{2, 2, 2, 2},
			{3, 3, 3, 3},
			{4, 4, 4, 4},
			{5, 5, 5, 5},
			{6, 6, 6, 6},
			{7, 7, 7, 7},
		}
		root1 := CalcMerkleRoot(leaves)

		// Calculate with different ordering
		reversed := make([]Hash, len(leaves))
		copy(reversed, leaves)
		for i := range len(reversed) / 2 {
			reversed[i], reversed[len(reversed)-1-i] = reversed[len(reversed)-1-i], reversed[i]
		}
		root2 := CalcMerkleRoot(reversed)

		require.NotEqual(t, root1, root2)
	})

	t.Run("one leaf", func(t *testing.T) {
		leaves := []Hash{{1, 1, 1, 1}}
		root := CalcMerkleRoot(leaves)

		require.Equal(t, root, leaves[0])
	})

	t.Run("nil slice", func(t *testing.T) {
		root := CalcMerkleRoot(nil)
		require.Equal(t, Hash{}, root)
	})

	t.Run("large number of leaves", func(t *testing.T) {
		leaves := make([]Hash, 1000)
		for i := range leaves {
			for j := range leaves[i] {
				leaves[i][j] = byte(i % 256)
			}
		}
		root := CalcMerkleRoot(leaves)
		require.NotEqual(t, Hash{}, root)

		// Verify deterministic output
		root2 := CalcMerkleRoot(leaves)
		require.Equal(t, root, root2)
	})
}

func TestSignature(t *testing.T) {
	t.Run("marshal and unmarshal", func(t *testing.T) {
		sig := &Signature{
			Data:       []byte("signature"),
			PubKey:     []byte("public-key"),
			PubKeyType: crypto.KeyTypeSecp256k1,
		}

		var buf bytes.Buffer
		n1, err := sig.WriteTo(&buf)
		require.NoError(t, err)

		var unmarshaled Signature
		n2, err := unmarshaled.ReadFrom(&buf)
		require.NoError(t, err)
		require.Equal(t, n1, n2)
		require.Equal(t, *sig, unmarshaled)
	})

	t.Run("empty signature", func(t *testing.T) {
		sig := &Signature{}

		var buf bytes.Buffer
		n1, err := sig.WriteTo(&buf)
		require.NoError(t, err)

		var unmarshaled Signature
		n2, err := unmarshaled.ReadFrom(&buf)
		require.NoError(t, err)
		require.Equal(t, n1, n2)
		require.Equal(t, *sig, unmarshaled)
	})

	t.Run("invalid signature length", func(t *testing.T) {
		sig := &Signature{
			Data:       []byte("signature"),
			PubKey:     []byte("public-key"),
			PubKeyType: crypto.KeyTypeSecp256k1,
		}

		var buf bytes.Buffer
		n1, err := sig.WriteTo(&buf)
		require.NoError(t, err)
		buf.Truncate(buf.Len() - 1) // Corrupt the data by truncating

		var unmarshaled Signature
		n2, err := unmarshaled.ReadFrom(&buf)
		require.Error(t, err)
		require.NotEqual(t, n1, n2)
	})
}

func TestVoteInfo(t *testing.T) {
	t.Run("marshal and unmarshal", func(t *testing.T) {
		vote := &VoteInfo{
			Signature: Signature{
				Data:       []byte("signature"),
				PubKey:     []byte("public-key"),
				PubKeyType: crypto.KeyTypeSecp256k1,
			},
			AckStatus: AckStatusDisagree,
		}

		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *vote, unmarshaled)
	})

	t.Run("empty vote info", func(t *testing.T) {
		vote := &VoteInfo{}

		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *vote, unmarshaled)
	})

	t.Run("invalid data length", func(t *testing.T) {
		vote := &VoteInfo{
			Signature: Signature{
				Data:       []byte("signature"),
				PubKey:     []byte("public-key"),
				PubKeyType: crypto.KeyTypeSecp256k1,
			},
			AckStatus: AckStatusDisagree,
		}
		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data[:len(data)-1])
		require.Error(t, err)
	})

	t.Run("Empty signature", func(t *testing.T) {
		vote := &VoteInfo{
			AckStatus: AckStatusDisagree,
		}

		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		// Corrupt the data by changing the signature length
		data = data[:len(data)-1]

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.Error(t, err)
	})

	t.Run("AckStatus Agreed", func(t *testing.T) {
		vote := &VoteInfo{
			Signature: Signature{
				Data:       []byte("signature"),
				PubKey:     []byte("public-key"),
				PubKeyType: crypto.KeyTypeSecp256k1,
			},
			AckStatus: AckStatusAgree,
		}

		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, vote, &unmarshaled)
	})

	t.Run("AckStatus Diverge With AppHash", func(t *testing.T) {
		hash := HashBytes([]byte("app-hash"))
		vote := &VoteInfo{
			Signature: Signature{
				Data:       []byte("signature"),
				PubKey:     []byte("public-key"),
				PubKeyType: crypto.KeyTypeSecp256k1,
			},
			AckStatus: AckStatusDiverge,
			AppHash:   &hash,
		}

		data, err := vote.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled VoteInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, vote, &unmarshaled)
	})

	t.Run("AckStatus Diverge Without AppHash", func(t *testing.T) {
		vote := &VoteInfo{
			Signature: Signature{
				Data:       []byte("signature"),
				PubKey:     []byte("public-key"),
				PubKeyType: crypto.KeyTypeSecp256k1,
			},
			AckStatus: AckStatusDiverge,
		}

		_, err := vote.MarshalBinary()
		require.Error(t, err)
	})
}

func TestSignAndVerifyVote(t *testing.T) {
	privKey, _, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)

	_, pubKey, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)

	blkID := HashBytes([]byte("test-block-id"))
	appHash := HashBytes([]byte("app-hash"))

	t.Run("Valid And Corrupted Vote", func(t *testing.T) {
		sig, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusAgree,
		}
		valid := vote.Verify(blkID, appHash)
		require.NoError(t, valid)

		// Corrupt the signature
		vote.Signature.Data[0]++
		valid = vote.Verify(blkID, appHash)
		require.Error(t, valid)
	})

	t.Run("Sign Ack Vote With Missing AppHash", func(t *testing.T) {
		_, err := SignVote(blkID, true, nil, privKey)
		require.Error(t, err)
	})

	t.Run("Missing AppHash with AckStatusDiverge", func(t *testing.T) {
		sig, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusDiverge,
		}
		// Vote is missing AppHash
		valid := vote.Verify(blkID, appHash)
		require.Error(t, valid)
	})

	t.Run("Incorrect AckStatus in the Vote", func(t *testing.T) {
		sig, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusDisagree, // Disagreed vote should be signed with Ack = false and without AppHash
		}

		valid := vote.Verify(blkID, appHash)
		require.Error(t, valid)
	})

	t.Run("Sign NACK vote without AppHash", func(t *testing.T) {
		sig, err := SignVote(blkID, false, nil, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusDisagree,
		}
		// Vote is missing AppHash
		valid := vote.Verify(blkID, appHash)
		require.NoError(t, valid)
	})

	t.Run("Sign NACK vote with AppHash", func(t *testing.T) {
		sig, err := SignVote(blkID, false, &appHash, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusDisagree,
		}
		// Vote is missing AppHash
		valid := vote.Verify(blkID, appHash)
		require.NoError(t, valid)
	})

	t.Run("Signed With Different Keys", func(t *testing.T) {
		sig, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)
		sig.PubKey = pubKey.Bytes()

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusAgree,
		}

		// Verify with different key
		valid := vote.Verify(blkID, appHash)
		require.Error(t, valid)
	})

	t.Run("SignVote with nil key", func(t *testing.T) {
		_, err := SignVote(blkID, true, &appHash, nil)
		require.Error(t, err)
	})
}

func TestCommitInfo(t *testing.T) {
	privKey, _, err := crypto.GenerateSecp256k1Key(nil)
	require.NoError(t, err)

	blkID := HashBytes([]byte("test-block-id"))
	appHash := HashBytes([]byte("app-hash"))

	t.Run("Zero Votes With Valid AppHash", func(t *testing.T) {
		commit := &CommitInfo{
			AppHash: appHash,
			Votes:   make([]*VoteInfo, 0),
		}
		data, err := commit.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled CommitInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *commit, unmarshaled)
	})

	t.Run("Zero Votes Without AppHash", func(t *testing.T) {
		commit := &CommitInfo{
			Votes: make([]*VoteInfo, 0),
		}
		data, err := commit.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled CommitInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *commit, unmarshaled)
	})

	t.Run("One Vote With AppHash", func(t *testing.T) {
		sig, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)

		vote := &VoteInfo{
			Signature: *sig,
			AckStatus: AckStatusAgree,
		}

		commitInfo := &CommitInfo{
			AppHash: appHash,
			Votes:   make([]*VoteInfo, 0),
		}
		commitInfo.Votes = append(commitInfo.Votes, vote)

		data, err := commitInfo.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled CommitInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *commitInfo, unmarshaled)
	})

	t.Run("More Than One Vote", func(t *testing.T) {
		sig1, err := SignVote(blkID, true, &appHash, privKey)
		require.NoError(t, err)

		sig2, err := SignVote(blkID, false, nil, privKey)
		require.NoError(t, err)

		vote1 := &VoteInfo{
			Signature: *sig1,
			AckStatus: AckStatusAgree,
		}

		vote2 := &VoteInfo{
			Signature: *sig2,
			AckStatus: AckStatusDisagree,
		}

		commitInfo := &CommitInfo{
			AppHash: appHash,
			Votes:   make([]*VoteInfo, 0),
		}
		commitInfo.Votes = append(commitInfo.Votes, vote1, vote2)

		data, err := commitInfo.MarshalBinary()
		require.NoError(t, err)

		var unmarshaled CommitInfo
		err = unmarshaled.UnmarshalBinary(data)
		require.NoError(t, err)
		require.Equal(t, *commitInfo, unmarshaled)
	})
}
