package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

type MerkleTree struct {
	Root      *MerkleNode
	Leaves    []*MerkleNode
	ProofType string
}

type MerkleNode struct {
	Hash   string
	Left   *MerkleNode
	Right  *MerkleNode
	Parent *MerkleNode
}

type BatchProof struct {
	RootHash      string      `json:"root_hash"`
	ProofType     string      `json:"proof_type"`
	LeafHashes    []string    `json:"leaf_hashes"`
	ProofElements []ProofNode `json:"proof_elements"`
}

type ProofNode struct {
	Hash   string `json:"hash"`
	IsLeft bool   `json:"is_left"`
}

func NewMerkleTree(signatures []string) *MerkleTree {
	if len(signatures) == 0 {
		return &MerkleTree{
			Root:      nil,
			Leaves:    []*MerkleNode{},
			ProofType: "merkle_tree",
		}
	}

	sortedSignatures := make([]string, len(signatures))
	copy(sortedSignatures, signatures)
	sort.Strings(sortedSignatures)

	leaves := make([]*MerkleNode, len(sortedSignatures))
	for i, sig := range sortedSignatures {
		leaves[i] = &MerkleNode{
			Hash: computeHash(sig),
		}
	}

	nodes := leaves
	for len(nodes) > 1 {
		var newLevel []*MerkleNode
		for i := 0; i < len(nodes); i += 2 {
			if i+1 < len(nodes) {
				combined := nodes[i].Hash + nodes[i+1].Hash
				parent := &MerkleNode{
					Hash:  computeHash(combined),
					Left:  nodes[i],
					Right: nodes[i+1],
				}
				nodes[i].Parent = parent
				nodes[i+1].Parent = parent
				newLevel = append(newLevel, parent)
			} else {
				combined := nodes[i].Hash + nodes[i].Hash
				parent := &MerkleNode{
					Hash:  computeHash(combined),
					Left:  nodes[i],
					Right: nodes[i],
				}
				nodes[i].Parent = parent
				newLevel = append(newLevel, parent)
			}
		}
		nodes = newLevel
	}

	return &MerkleTree{
		Root:      nodes[0],
		Leaves:    leaves,
		ProofType: "merkle_tree",
	}
}

func (mt *MerkleTree) GetRootHash() string {
	if mt.Root == nil {
		return ""
	}
	return mt.Root.Hash
}

func (mt *MerkleTree) GenerateBatchProof(targetSignatures []string) *BatchProof {
	if mt.Root == nil || len(targetSignatures) == 0 {
		return nil
	}

	sortedSignatures := make([]string, len(targetSignatures))
	copy(sortedSignatures, targetSignatures)
	sort.Strings(sortedSignatures)

	leafHashes := make([]string, len(sortedSignatures))
	for i, sig := range sortedSignatures {
		leafHashes[i] = computeHash(sig)
	}

	proofElements := make([]ProofNode, 0)

	for _, targetHash := range leafHashes {
		proof := mt.buildProof(targetHash)
		proofElements = append(proofElements, proof...)
	}

	return &BatchProof{
		RootHash:      mt.GetRootHash(),
		ProofType:     mt.ProofType,
		LeafHashes:    leafHashes,
		ProofElements: proofElements,
	}
}

func (mt *MerkleTree) buildProof(targetHash string) []ProofNode {
	proof := make([]ProofNode, 0)
	currentNode := mt.findLeafNode(targetHash)

	if currentNode == nil {
		return proof
	}

	node := currentNode
	for node != nil && node.Parent != nil {
		parent := node.Parent
		if parent.Left == node {
			if parent.Right != nil {
				proof = append(proof, ProofNode{
					Hash:   parent.Right.Hash,
					IsLeft: false,
				})
			}
		} else {
			if parent.Left != nil {
				proof = append(proof, ProofNode{
					Hash:   parent.Left.Hash,
					IsLeft: true,
				})
			}
		}
		node = parent
	}

	return proof
}

func (mt *MerkleTree) findLeafNode(targetHash string) *MerkleNode {
	for _, leaf := range mt.Leaves {
		if leaf.Hash == targetHash {
			return leaf
		}
	}
	return nil
}

func (mt *MerkleTree) VerifyProof(proof *BatchProof) bool {
	if proof == nil || mt.Root == nil {
		return false
	}

	if proof.RootHash != mt.Root.Hash {
		return false
	}

	computedRoot := mt.computeRootFromProof(proof.LeafHashes, proof.ProofElements)
	return computedRoot == proof.RootHash
}

func (mt *MerkleTree) computeRootFromProof(leafHashes []string, proofElements []ProofNode) string {
	if len(leafHashes) == 0 {
		return ""
	}

	currentHashes := make([]string, len(leafHashes))
	copy(currentHashes, leafHashes)

	proofIdx := 0
	for len(currentHashes) > 1 {
		var newHashes []string
		for i := 0; i < len(currentHashes); i += 2 {
			if i+1 < len(currentHashes) {
				var leftHash, rightHash string
				if proofIdx < len(proofElements) {
					pn := proofElements[proofIdx]
					if pn.IsLeft {
						leftHash = pn.Hash
						rightHash = currentHashes[i]
					} else {
						leftHash = currentHashes[i]
						rightHash = pn.Hash
					}
					proofIdx++
				} else {
					leftHash = currentHashes[i]
					rightHash = currentHashes[i+1]
				}
				combined := leftHash + rightHash
				newHashes = append(newHashes, computeHash(combined))
			} else {
				if proofIdx < len(proofElements) {
					pn := proofElements[proofIdx]
					combined := currentHashes[i] + pn.Hash
					newHashes = append(newHashes, computeHash(combined))
					proofIdx++
				} else {
					combined := currentHashes[i] + currentHashes[i]
					newHashes = append(newHashes, computeHash(combined))
				}
			}
		}
		currentHashes = newHashes
	}

	if len(currentHashes) > 0 {
		return currentHashes[0]
	}
	return ""
}

func computeHash(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

type MerkleTreeBuilder struct {
	signatures []string
}

func NewMerkleTreeBuilder() *MerkleTreeBuilder {
	return &MerkleTreeBuilder{
		signatures: make([]string, 0),
	}
}

func (b *MerkleTreeBuilder) AddSignature(signature string) *MerkleTreeBuilder {
	if signature != "" {
		b.signatures = append(b.signatures, signature)
	}
	return b
}

func (b *MerkleTreeBuilder) Build() *MerkleTree {
	return NewMerkleTree(b.signatures)
}

func (b *MerkleTreeBuilder) BuildProof(targetSignatures []string) *BatchProof {
	tree := b.Build()
	return tree.GenerateBatchProof(targetSignatures)
}

type ExtendedMerkleTree struct {
	*MerkleTree
	Timestamp     int64  `json:"timestamp"`
	PreviousRoot  string `json:"previous_root"`
	ChainSequence int    `json:"chain_sequence"`
}

func NewExtendedMerkleTree(signatures []string, previousRoot string, sequence int) *ExtendedMerkleTree {
	tree := NewMerkleTree(signatures)
	return &ExtendedMerkleTree{
		MerkleTree:    tree,
		Timestamp:     0,
		PreviousRoot:  previousRoot,
		ChainSequence: sequence,
	}
}

func (emt *ExtendedMerkleTree) GetAggregateHash() string {
	if emt.Root == nil {
		return ""
	}
	combined := emt.Root.Hash + emt.PreviousRoot
	return computeHash(combined)
}
