package node

import (
	"bufio"
	"bytes"
	"context"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	ktypes "github.com/kwilteam/kwil-db/core/types"
	"github.com/kwilteam/kwil-db/node/peers"
	"github.com/kwilteam/kwil-db/node/types"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

type (
	ConsensusReset = types.ConsensusReset
	AckRes         = types.AckRes
	DiscReq        = types.DiscoveryRequest
	DiscRes        = types.DiscoveryResponse
)

type blockProp struct {
	Height    int64
	Hash      types.Hash
	PrevHash  types.Hash
	Stamp     int64
	LeaderSig []byte
	// Replacing *types.Hash
}

func (bp blockProp) String() string {
	return fmt.Sprintf("prop{height:%d hash:%s prevHash:%s}",
		bp.Height, bp.Hash, bp.PrevHash)
}

var _ encoding.BinaryMarshaler = blockProp{}
var _ encoding.BinaryMarshaler = (*blockProp)(nil)

func (bp blockProp) MarshalBinary() ([]byte, error) {
	// 8 bytes for int64 + 2 hash lengths + 8 bytes for time stamp + len(sig) + sig
	buf := make([]byte, 8+2*types.HashLen+8+8+len(bp.LeaderSig))
	var c int
	binary.LittleEndian.PutUint64(buf[:8], uint64(bp.Height))
	c += 8
	copy(buf[c:], bp.Hash[:])
	c += types.HashLen
	copy(buf[c:], bp.PrevHash[:])
	c += types.HashLen
	binary.LittleEndian.PutUint64(buf[c:], uint64(bp.Stamp))
	c += 8
	binary.LittleEndian.PutUint64(buf[c:], uint64(len(bp.LeaderSig)))
	c += 8
	copy(buf[c:], bp.LeaderSig) // c += len(bp.LeaderSig)
	return buf, nil

	// NOTE: this can be written in terms of WriteTo, but it is more efficient
	// given that we know the required lengths.
	// var buf bytes.Buffer
	// _, err := bp.WriteTo(&buf)
	// if err != nil {
	// 	return nil, err
	// }
	// return buf.Bytes(), nil
}

func (bp *blockProp) UnmarshalBinary(data []byte) error {
	_, err := bp.ReadFrom(bytes.NewReader(data))
	return err
}

var _ io.ReaderFrom = (*blockProp)(nil)

func (bp *blockProp) ReadFrom(r io.Reader) (int64, error) {
	var n int64
	if err := binary.Read(r, binary.LittleEndian, &bp.Height); err != nil {
		return n, err
	}
	n += 8
	nr, err := io.ReadFull(r, bp.Hash[:])
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	nr, err = io.ReadFull(r, bp.PrevHash[:])
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	if err := binary.Read(r, binary.LittleEndian, &bp.Stamp); err != nil {
		return n, err
	}
	n += 8
	var sigLen int64
	if err := binary.Read(r, binary.LittleEndian, &sigLen); err != nil {
		return n, err
	}
	n += 8
	if sigLen > 1000 { // TODO: smarter sanity check
		return n, errors.New("invalid signature length")
	}
	bp.LeaderSig = make([]byte, sigLen)
	nr, err = io.ReadFull(r, bp.LeaderSig)
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	return n, nil
}

var _ io.WriterTo = (*blockProp)(nil)

func (bp *blockProp) WriteTo(w io.Writer) (int64, error) {
	data, err := bp.MarshalBinary()
	if err != nil {
		return 0, err
	}
	nr, err := w.Write(data)
	return int64(nr), err

	// NOTE: this can be written using binary.Write etc., but this may not be
	// worth the maintenance cost, particularly if it is actually more efficient
	// in terms of consolidating network writes.

	/*var n int64
	if err := binary.Write(w, binary.LittleEndian, bp.Height); err != nil {
		return n, err
	}
	n += 8
	nr, err := w.Write(bp.Hash[:])
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	nr, err = w.Write(bp.PrevHash[:])
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	if err := binary.Write(w, binary.LittleEndian, bp.Stamp); err != nil {
		return n, err
	}
	n += 8
	if err := binary.Write(w, binary.LittleEndian, int64(len(bp.LeaderSig))); err != nil {
		return n, err
	}
	n += 8
	nr, err = w.Write(bp.LeaderSig)
	if err != nil {
		return int64(nr), err
	}
	n += int64(nr)
	return n, nil*/
}

func (n *Node) announceBlkProp(ctx context.Context, blk *ktypes.Block) {
	rawBlk := ktypes.EncodeBlock(blk)
	blkHash := blk.Hash()
	height := blk.Header.Height

	n.log.Debug("announcing proposed block", "hash", blkHash, "height", height,
		"txs", len(blk.Txns), "size", len(rawBlk))

	peers := n.peers()
	if len(peers) == 0 {
		n.log.Warnf("no peers to advertise block to")
		return
	}

	me := n.host.ID()
	for _, peerID := range peers {
		if peerID == me {
			continue
		}
		prop := blockProp{Height: height, Hash: blkHash, PrevHash: blk.Header.PrevHash,
			Stamp: blk.Header.Timestamp.UnixMilli(), LeaderSig: blk.Signature}
		n.log.Debugf("advertising block proposal %s (height %d / txs %d) to peer %v", blkHash, height, len(blk.Txns), peerID)
		// resID := annPropMsgPrefix + strconv.Itoa(int(height)) + ":" + prevHash + ":" + blkid
		propID, _ := prop.MarshalBinary()
		err := n.advertiseToPeer(ctx, peerID, ProtocolIDBlockPropose, contentAnn{prop.String(), propID, rawBlk},
			blkSendTimeout)
		if err != nil {
			n.log.Infof(err.Error())
			continue
		}
	}
}

// blkPropStreamHandler is the stream handler for the ProtocolIDBlockPropose
// protocol i.e. proposed block announcements, which originate from the leader,
// but may be re-announced by other validators.
//
// This stream should:j
//  1. provide the announcement to the consensus engine (CE)
//  2. if the CE rejects the ann, close stream
//  3. if the CE is ready for this proposed block, request the block
//  4. provide the block contents to the CE
//  5. close the stream
//
// Note that CE decides what to do. For instance, after we provide the full
// block contents, the CE will likely begin executing the blocks. When it is
// done, it will send an ACK/NACK with the
func (n *Node) blkPropStreamHandler(s network.Stream) {
	defer s.Close()

	// if n.role.Load() == types.RoleLeader {
	// 	return
	// }

	var prop blockProp
	_, err := prop.ReadFrom(s)
	if err != nil {
		n.log.Warnf("invalid block proposal message: %v", err)
		return
	}

	height := prop.Height

	if !n.ce.AcceptProposal(height, prop.Hash, prop.PrevHash, prop.LeaderSig, prop.Stamp) {
		// NOTE: if this is ahead of our last commit height, we have to try to catch up
		n.log.Debug("do not want proposal content", "height", height, "hash", prop.Hash,
			"prevHash", prop.PrevHash)
		return
	}

	_, err = s.Write([]byte(getMsg))
	if err != nil {
		n.log.Warnf("failed to request block proposal contents: %w", err)
		return
	}

	rd := bufio.NewReader(s)
	blkProp, err := io.ReadAll(rd)
	if err != nil {
		n.log.Warnf("failed to read block proposal contents: %w", err)
		return
	}

	// Q: header first, or full serialized block?

	blk, err := ktypes.DecodeBlock(blkProp)
	if err != nil {
		n.log.Warnf("decodeBlock failed for proposal at height %d: %v", height, err)
		return
	}
	if blk.Header.Height != height {
		n.log.Warnf("unexpected height: wanted %d, got %d", height, blk.Header.Height)
		return
	}

	annHash := prop.Hash
	hash := blk.Header.Hash()
	if hash != annHash {
		n.log.Warnf("unexpected hash: wanted %s, got %s", hash, annHash)
		return
	}

	n.log.Info("processing block proposal", "height", height, "hash", hash)

	n.ce.NotifyBlockProposal(blk)
}

// sendACK is a callback for the result of validator block execution/precommit.
// After then consensus engine executes the block, this is used to gossip the
// result back to the leader.
func (n *Node) sendACK(ack bool, height int64, blkID types.Hash, appHash *types.Hash, signature []byte) error {
	// n.log.Debugln("sending ACK", height, ack, blkID, appHash)
	n.ackChan <- types.AckRes{
		ACK:     ack,
		AppHash: appHash,
		BlkHash: blkID,
		Height:  height,

		Signature:  signature,
		PubKeyType: n.pubkey.Type(),
		PubKey:     n.pubkey.Bytes(),
	}
	return nil // actually gossip the nack
}

func (n *Node) startAckGossip(ctx context.Context, ps *pubsub.PubSub) error {
	topicAck, subAck, err := subTopic(ctx, ps, TopicACKs)
	if err != nil {
		return err
	}

	subCanceled := make(chan struct{})

	n.wg.Add(1)
	go func() {
		defer func() {
			<-subCanceled
			topicAck.Close()
			n.wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ack := <-n.ackChan:
				n.log.Debugln("publishing ACK", ack.ACK, ack.Height, ack.BlkHash, ack.AppHash)
				ackMsg, _ := ack.MarshalBinary()
				err := topicAck.Publish(ctx, ackMsg)
				if err != nil {
					n.log.Warnf("Publish ACK failure (%v for %v): %v", ack.ACK, ack.BlkHash, err)
					// TODO: queue the ack for retry (send back to ackChan or another delayed send queue)
					return
				}
			}

		}
	}()

	me := n.host.ID()

	go func() {
		defer close(subCanceled)
		defer subAck.Cancel()
		for {
			ackMsg, err := subAck.Next(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					n.log.Infof("subTx.Next:", err)
				}
				return
			}

			// We're only interested if we are the leader.
			if n.ce.Role() != types.RoleLeader {
				// n.log.Debugln("discarding ack meant for leader")
				continue // discard, we are just relaying to leader
			}

			if peer.ID(ackMsg.From) == me {
				// n.log.Infof("ACK message from me ignored")
				continue
			}

			var ack AckRes
			err = ack.UnmarshalBinary(ackMsg.Data)
			if err != nil {
				n.log.Infof("failed to decode ACK msg: %v", err)
				continue
			}
			fromPeerID := ackMsg.GetFrom()

			n.log.Debugf("received ACK msg from %s (rcvd from %s), data = %x",
				fromPeerID.String(), ackMsg.ReceivedFrom.String(), ackMsg.Message.Data)

			peerPubKey, err := peers.PubKeyFromPeerID(fromPeerID.String())
			if err != nil {
				n.log.Infof("failed to extract pubkey from peer ID %v: %v", fromPeerID, err)
				continue
			}
			pubkeyBytes := peerPubKey.Bytes() // does not error for secp256k1 or ed25519
			go n.ce.NotifyACK(pubkeyBytes, ack)
		}
	}()

	return nil
}

func (n *Node) sendDiscoveryRequest() {
	n.log.Debug("sending Discovery request")
	n.discReq <- types.DiscoveryRequest{}
}

func (n *Node) sendDiscoveryResponse(bestHeight int64) {
	n.log.Debug("sending Discovery response", "height", bestHeight)
	n.discResp <- types.DiscoveryResponse{BestHeight: bestHeight}
}

func (n *Node) startDiscoveryRequestGossip(ctx context.Context, ps *pubsub.PubSub) error {
	topicDisc, subDisc, err := subTopic(ctx, ps, TopicDiscReq)
	if err != nil {
		return err
	}

	subCanceled := make(chan struct{})
	n.log.Info("starting Discovery request gossip")

	n.wg.Add(1)
	go func() {
		defer func() {
			<-subCanceled
			topicDisc.Close()
			n.wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-n.discReq:
				n.log.Debugln("publishing Discovery request")
				err := topicDisc.Publish(ctx, nil)
				if err != nil {
					n.log.Warnf("Publish Discovery request failure: %v", err)
					return
				}
			}
		}
	}()

	me := n.host.ID()

	go func() {
		defer close(subCanceled)
		defer subDisc.Cancel()
		for {
			discMsg, err := subDisc.Next(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					n.log.Infof("subTx.Next:", err)
				}
				return
			}

			if peer.ID(discMsg.From) == me {
				continue
			}

			n.log.Infof("received Discovery request from %s", discMsg.ReceivedFrom.String())

			// Check the block store for the best height and respond
			bestHeight, _, _ := n.bki.Best()
			n.sendDiscoveryResponse(bestHeight)

			n.log.Info("responded to Discovery request", "height", bestHeight)
		}
	}()

	return nil
}

func (n *Node) startDiscoveryResponseGossip(ctx context.Context, ps *pubsub.PubSub) error {
	topicDisc, subDisc, err := subTopic(ctx, ps, TopicDiscResp)
	if err != nil {
		return err
	}

	subCanceled := make(chan struct{})

	n.log.Info("starting Discovery response gossip")

	n.wg.Add(1)
	go func() {
		defer func() {
			<-subCanceled
			topicDisc.Close()
			n.wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-n.discResp:
				n.log.Debugln("publishing Discovery Response message", msg.BestHeight)
				discMsg, _ := msg.MarshalBinary()
				err := topicDisc.Publish(ctx, discMsg)
				if err != nil {
					n.log.Warnf("Publish Discovery resp failure (%v): %v", msg.BestHeight, err)
					return
				}
			}

		}
	}()

	me := n.host.ID()

	go func() {
		defer close(subCanceled)
		defer subDisc.Cancel()
		for {
			discMsg, err := subDisc.Next(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					n.log.Infof("subTx.Next:", err)
				}
				return
			}

			// We're only interested if we are the leader.
			if n.ce.Role() != types.RoleLeader {
				continue // discard, we are just relaying to leader
			}

			if peer.ID(discMsg.From) == me {
				continue
			}

			var dm DiscRes
			err = dm.UnmarshalBinary(discMsg.Data)
			if err != nil {
				n.log.Infof("failed to decode Discovery msg: %v", err)
				continue
			}
			fromPeerID := discMsg.GetFrom()

			n.log.Infof("received Discovery response msg from %s (rcvd from %s), data = %d",
				fromPeerID.String(), discMsg.ReceivedFrom.String(), dm.BestHeight)

			peerPubKey, err := peers.PubKeyFromPeerID(fromPeerID.String())
			if err != nil {
				n.log.Infof("failed to extract pubkey from peer ID %v: %v", fromPeerID, err)
				continue
			}
			pubkeyBytes := peerPubKey.Bytes() // does not error for secp256k1 or ed25519
			go n.ce.NotifyDiscoveryMessage(pubkeyBytes, dm.BestHeight)
		}
	}()

	return nil
}

func (n *Node) sendReset(height int64, txIDs []ktypes.Hash) error {
	n.resetMsg <- types.ConsensusReset{
		ToHeight: height,
		TxIDs:    txIDs,
	}
	return nil
}

func subConsensusReset(ctx context.Context, ps *pubsub.PubSub) (*pubsub.Topic, *pubsub.Subscription, error) {
	return subTopic(ctx, ps, TopicReset)
}

func (n *Node) startConsensusResetGossip(ctx context.Context, ps *pubsub.PubSub) error {
	topicReset, subReset, err := subConsensusReset(ctx, ps)
	if err != nil {
		return err
	}

	subCanceled := make(chan struct{})

	n.wg.Add(1)
	go func() {
		defer func() {
			<-subCanceled
			topicReset.Close()
			n.wg.Done()
		}()
		for {
			var resetMsg ConsensusReset
			select {
			case <-ctx.Done():
				return
			case resetMsg = <-n.resetMsg:
			}

			err := topicReset.Publish(ctx, resetMsg.Bytes())
			if err != nil {
				return
			}
		}
	}()

	me := n.host.ID()

	go func() {
		defer close(subCanceled)
		defer subReset.Cancel()
		for {
			resetMsg, err := subReset.Next(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					n.log.Errorf("Stopping Consensus Reset gossip!", "error", err)
				}
				return
			}

			if string(resetMsg.From) == string(me) {
				continue
			}

			var reset ConsensusReset
			err = reset.UnmarshalBinary(resetMsg.Data)
			if err != nil {
				n.log.Errorf("unable to unmarshal reset msg: %v", err)
				continue
			}

			fromPeerID := resetMsg.GetFrom()

			n.log.Infof("received Consensus Reset msg from %s (rcvd from %s), data = %x",
				fromPeerID, resetMsg.ReceivedFrom, resetMsg.Message.Data)

			// source of the reset message should be the leader
			peerPubKey, err := peers.PubKeyFromPeerID(fromPeerID.String())
			if err != nil {
				n.log.Infof("failed to extract pubkey from peer ID %v: %v", fromPeerID, err)
				continue
			}
			pubkeyBytes := peerPubKey.Bytes()

			n.ce.NotifyResetState(reset.ToHeight, reset.TxIDs, pubkeyBytes)
		}
	}()

	return nil
}
