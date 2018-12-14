package explorer

import (
	"fmt"
	"os"
	"sync"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/harmony-one/harmony/core/types"
	"github.com/harmony-one/harmony/db"
)

var storage *Storage
var once sync.Once

// Storage dump the block info into leveldb.
type Storage struct {
	db *db.LDBDatabase
}

// GetStorageInstance returns attack model by using singleton pattern.
func GetStorageInstance(ip, port string) *Storage {
	once.Do(func() {
		storage = &Storage{}
		storage.Init(ip, port)
	})
	return storage
}

// Init initializes the block update.
func (storage *Storage) Init(ip, port string) {
	dbFileName := "/tmp/explorer_storage_" + ip + "_" + port
	var err = os.RemoveAll(dbFileName)
	if err != nil {
		fmt.Println(err.Error())
	}
	if storage.db, err = db.NewLDBDatabase(dbFileName, 0, 0); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

// Dump extracts information from block and index them into lvdb for explorer.
func (storage *Storage) Dump(accountBlock []byte, height uint32) {
	fmt.Println("Dumping block ", height)
	if accountBlock == nil {
		return
	}
	block := new(types.Block)
	rlp.DecodeBytes(accountBlock, block)

	// Store block.
	storage.db.Put([]byte(fmt.Sprintf("b_%d", height)), accountBlock)

	// Store block info.
	blockInfo := BlockInfo{
		ID:        block.Hash().Hex(),
		Height:    string(height),
		Timestamp: string(block.Time().Int64()),
		TXCount:   string(block.Transactions().Len()),
		Size:      block.Size().String(),
	}

	if data, err := rlp.EncodeToBytes(blockInfo); err == nil {
		storage.db.Put([]byte(fmt.Sprintf("bi_%d", height)), data)
	} else {
		fmt.Println("EncodeRLP blockInfo error")
		os.Exit(1)
	}

	// Store txs
	for _, tx := range block.Transactions() {
		if data, err := rlp.EncodeToBytes(tx); err == nil {
			storage.db.Put([]byte(fmt.Sprintf("tx_%s", tx.Hash().Hex())), data)
		} else {
			fmt.Println("EncodeRLP transaction error")
			os.Exit(1)
		}
	}
}