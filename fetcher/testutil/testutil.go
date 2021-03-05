package testutil

import (
	"bytes"
	"fmt"
	"io"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	_ "github.com/ipld/go-ipld-prime/codec/dagcbor"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
)

// EncodeBlock produces an encoded block from a node
func EncodeBlock(n ipld.Node) (blocks.Block, ipld.Node, ipld.Link) {
	ls := cidlink.DefaultLinkSystem()
	var b blocks.Block
	lb := cidlink.LinkPrototype{cid.Prefix{
		Version:  1,
		Codec:    0x71,
		MhType:   0x17,
		MhLength: 20,
	}}
	ls.StorageWriteOpener = func(ipld.LinkContext) (io.Writer, ipld.BlockWriteCommitter, error) {
		buf := bytes.Buffer{}
		return &buf, func(lnk ipld.Link) error {
			clnk, ok := lnk.(cidlink.Link)
			if !ok {
				return fmt.Errorf("incorrect link type %v", lnk)
			}
			var err error
			b, err = blocks.NewBlockWithCid(buf.Bytes(), clnk.Cid)
			return err
		}, nil
	}
	lnk, err := ls.Store(ipld.LinkContext{}, lb, n)
	if err != nil {
		panic(err)
	}
	return b, n, lnk
}
