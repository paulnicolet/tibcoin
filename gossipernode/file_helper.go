package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/paulnicolet/Peerster/part2/common"
	"io"
	"os"
	"path/filepath"
)

func (gossiper *Gossiper) initFile(name string, metafile []byte, metahash []byte) *common.File {
	return &common.File{
		Name:     name,
		MetaFile: metafile,
		MetaHash: metahash,
		Content:  make(map[uint64][]byte),
	}
}

func (gossiper *Gossiper) getFile(filename string) (*common.File, bool) {
	gossiper.filesMutex.Lock()
	defer gossiper.filesMutex.Unlock()
	file, in := gossiper.files[filename]
	return file, in
}

func (gossiper *Gossiper) getChunkHash(file *common.File, idx uint64) []byte {
	lowIdx := idx * common.HashSize
	highIdx := (idx + 1) * common.HashSize
	return file.MetaFile[lowIdx:highIdx]
}

func (gossiper *Gossiper) getChunkIdx(file *common.File, hash []byte) (uint64, error) {
	nChunk := len(file.MetaFile) / common.HashSize

	for chunkIdx := 0; chunkIdx < nChunk; chunkIdx++ {
		if bytes.Equal(hash, file.MetaFile[chunkIdx*common.HashSize:(chunkIdx+1)*common.HashSize]) {
			return uint64(chunkIdx), nil
		}
	}

	return 0, fmt.Errorf("No chunk corresponding to the given hash")
}

func (gossiper *Gossiper) getChunk(file *common.File, idx uint64) []byte {
	return file.Content[idx]
}

func (gossiper *Gossiper) buildFile(filename string) (*common.File, error) {
	// Open file
	path, err := filepath.Abs(common.FilePath + filename)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var metafile []byte
	content := make(map[uint64][]byte)

	// Read file by chunk
	var chunkIdx uint64 = 0
	for {
		buffer := make([]byte, common.ChunkSize)
		n, err := f.Read(buffer)
		if err == io.EOF {
			break
		}

		// Cut chunk if end of file
		chunk := buffer[:n]

		// Store file chunk
		content[chunkIdx] = chunk
		chunkIdx++

		// Append chunk has to metafile
		hash := sha256.Sum256(chunk)
		metafile = append(metafile, hash[:]...)
	}

	// Compute metahash
	metahash := sha256.Sum256(metafile)

	// Build file structutre
	file := common.File{
		Name:     filename,
		Content:  content,
		MetaFile: metafile,
		MetaHash: metahash[:],
	}

	gossiper.errLogger.Printf("Uploaded file with metahash: %s", hex.EncodeToString(metahash[:]))

	return &file, nil
}

func (gossiper *Gossiper) archiveFile(file *common.File) error {
	gossiper.stdLogger.Printf("RECONSTRUCTED file %s", file.Name)
	f, err := os.Create(common.DownloadPath + file.Name)
	if err != nil {
		return err
	}

	var content []byte
	nChunk := len(file.MetaFile) / common.HashSize
	for i := 0; i < nChunk; i++ {
		content = append(content, file.Content[uint64(i)]...)
	}

	_, err = f.Write(content)
	if err != nil {
		return err
	}

	return nil
}

func (gossiper *Gossiper) alreadyMatched(filename string, matches []*FileSearchState) bool {
	for _, state := range matches {
		if filename == state.FileName {
			return true
		}
	}

	return false
}

func (gossiper *Gossiper) requestTicker(timeout *Timeout, privatePacket *common.PrivatePacket, to string) {
	for {
		select {
		case <-timeout.Ticker.C:
			gossiper.sendPrivateNoTimeout(privatePacket, to)
		case <-timeout.Killer:
			return
		}
	}
}
