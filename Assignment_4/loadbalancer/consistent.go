// Partner 1: 2020-10-0148
// Partner 2: 2020-10-0287

package loadbalancer

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
)

// ConsistentHashing contains any fields that are required to maintain
// the consistent hash ring
type ConsistentHashing struct {
	// TODO: Implement this!
}

// Hash calculates hash of the input key using SHA-256. DO NOT MODIFY!
func (c *ConsistentHashing) Hash(key string) uint64 {
	hasher := sha256.New()
	hasher.Write([]byte(key))
	digest := hasher.Sum(nil)
	digestAsUint64 := binary.LittleEndian.Uint64(digest)
	return digestAsUint64
}

// VirtualNodeHash returns the hash of the nth virtual node of a given
// physical node. DO NOT MODIFY!
//
// Parameters:  key - physical node's key
// 				n - nth virtual node (Possible values: 1, 2, 3, etc.)
func (c *ConsistentHashing) VirtualNodeHash(key string, n int) uint64 {
	return c.Hash(strconv.Itoa(n) + "-" + key)
}

// TODO: add additional methods/functions below!
