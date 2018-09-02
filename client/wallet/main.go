package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"github.com/dedis/kyber"
	"github.com/simple-rules/harmony-benchmark/blockchain"
	"github.com/simple-rules/harmony-benchmark/client"
	"github.com/simple-rules/harmony-benchmark/configr"
	"github.com/simple-rules/harmony-benchmark/crypto"
	"github.com/simple-rules/harmony-benchmark/crypto/pki"
	"github.com/simple-rules/harmony-benchmark/node"
	"github.com/simple-rules/harmony-benchmark/p2p"
	proto_node "github.com/simple-rules/harmony-benchmark/proto/node"
	"github.com/simple-rules/harmony-benchmark/utils"
	"io"
	"io/ioutil"
	math_rand "math/rand"
	"os"
	"strconv"
	"time"
)

func main() {
	// Account subcommands
	accountImportCommand := flag.NewFlagSet("import", flag.ExitOnError)
	transferCommand := flag.NewFlagSet("transfer", flag.ExitOnError)
	//accountListCommand := flag.NewFlagSet("list", flag.ExitOnError)
	//
	//// Transaction subcommands
	//transactionNewCommand := flag.NewFlagSet("new", flag.ExitOnError)
	//
	//// Account subcommand flag pointers
	//// Adding a new choice for --metric of 'substring' and a new --substring flag
	accountImportPtr := accountImportCommand.String("privateKey", "", "Specify the private key to import")

	transferSenderPtr := transferCommand.String("sender", "0", "Specify the sender account address or index")
	transferReceiverPtr := transferCommand.String("receiver", "", "Specify the receiver account")
	transferAmountPtr := transferCommand.Int("amount", 0, "Specify the amount to transfer")
	//accountListPtr := accountNewCommand.Bool("new", false, "N/A")
	//
	//// Transaction subcommand flag pointers
	//transactionNewPtr := transactionNewCommand.String("text", "", "Text to parse. (Required)")

	// Verify that a subcommand has been provided
	// os.Arg[0] is the main command
	// os.Arg[1] will be the subcommand
	if len(os.Args) < 2 {
		fmt.Println("account or transaction subcommand is required")
		os.Exit(1)
	}

	// Switch on the subcommand
	// Parse the flags for appropriate FlagSet
	// FlagSet.Parse() requires a set of arguments to parse as input
	// os.Args[2:] will be all arguments starting after the subcommand at os.Args[1]
	switch os.Args[1] {
	case "account":
		switch os.Args[2] {
		case "new":
			fmt.Println("Creating new account...")

			randomBytes := [32]byte{}
			_, err := io.ReadFull(rand.Reader, randomBytes[:])

			if err != nil {
				fmt.Println("Failed to create a new private key...")
				return
			}
			priKey := crypto.Ed25519Curve.Scalar().SetBytes(randomBytes[:])
			priKeyBytes, err := priKey.MarshalBinary()
			if err != nil {
				panic("Failed to generate private key")
			}
			pubKey := pki.GetPublicKeyFromScalar(priKey)
			address := pki.GetAddressFromPublicKey(pubKey)
			StorePrivateKey(priKeyBytes)
			fmt.Printf("New account created:\nAddress: {%x}\n", address)
		case "list":
			for i, address := range ReadAddresses() {
				fmt.Printf("Account %d:\n {%x}\n", i+1, address)
			}
		case "clearAll":
			fmt.Println("Deleting existing accounts...")
			DeletePrivateKey()
		case "import":
			fmt.Println("Importing private key...")
			accountImportCommand.Parse(os.Args[3:])
			priKey := *accountImportPtr
			if !accountImportCommand.Parsed() {
				fmt.Println("Failed to parse flags")
			}
			priKeyBytes, err := hex.DecodeString(priKey)
			if err != nil {
				panic("Failed to parse the private key into bytes")
			}
			StorePrivateKey(priKeyBytes)
		case "showBalance":
			configr := configr.NewConfigr()
			configr.ReadConfigFile("local_config_shards.txt")
			leaders, _ := configr.GetLeadersAndShardIds()
			clientPeer := configr.GetClientPeer()
			walletNode := node.New(nil, nil)
			walletNode.Client = client.NewClient(&leaders)
			walletNode.ClientPeer = clientPeer
			go walletNode.StartServer(clientPeer.Port)

			shardUtxoMap, err := FetchUtxos(ReadAddresses(), walletNode)
			if err != nil {
				fmt.Println(err)
			}
			PrintUtxoBalance(shardUtxoMap)
		case "test":
			priKey := pki.GetPrivateKeyScalarFromInt(444)
			address := pki.GetAddressFromPrivateKey(priKey)
			priKeyBytes, err := priKey.MarshalBinary()
			if err != nil {
				panic("Failed to deserialize private key scalar.")
			}
			fmt.Printf("Private Key :\n {%x}\n", priKeyBytes)
			fmt.Printf("Address :\n {%x}\n", address)
		}
	case "transfer":
		transferCommand.Parse(os.Args[2:])
		if !transferCommand.Parsed() {
			fmt.Println("Failed to parse flags")
		}
		sender := *transferSenderPtr
		receiver := *transferReceiverPtr
		amount := *transferAmountPtr

		if amount <= 0 {
			fmt.Println("Please specify positive amount to transfer")
		}
		priKeys := ReadPrivateKeys()
		if len(priKeys) == 0 {
			fmt.Println("No existing account to send money from.")
			return
		}
		senderIndex, err := strconv.Atoi(sender)
		senderAddress := ""
		addresses := ReadAddresses()
		if err != nil {
			senderIndex = -1
			for i, address := range addresses {
				if fmt.Sprintf("%x", address) == senderAddress {
					senderIndex = i
					break
				}
			}
			if senderIndex == -1 {
				fmt.Println("Specified sender account is not imported yet.")
				break
			}
		}
		if senderIndex >= len(priKeys) {
			fmt.Println("Sender account index out of bounds.")
			return
		}
		receiverAddress, err := hex.DecodeString(receiver)
		if err != nil || len(receiverAddress) != 20 {
			fmt.Println("The receiver address is not a valid address.")
			return
		}

		// Generate transaction
		trimmedReceiverAddress := [20]byte{}
		copy(trimmedReceiverAddress[:], receiverAddress[:20])

		senderPriKey := priKeys[senderIndex]
		senderAddressBytes := pki.GetAddressFromPrivateKey(senderPriKey)

		// Start client server
		configr := configr.NewConfigr()
		configr.ReadConfigFile("local_config_shards.txt")
		leaders, _ := configr.GetLeadersAndShardIds()
		clientPeer := configr.GetClientPeer()
		walletNode := node.New(nil, nil)
		walletNode.Client = client.NewClient(&leaders)
		walletNode.ClientPeer = clientPeer
		go walletNode.StartServer(clientPeer.Port)

		shardUtxoMap, err := FetchUtxos([][20]byte{senderAddressBytes}, walletNode)
		if err != nil {
			fmt.Printf("Failed to fetch utxos: %s\n", err)
		}

		cummulativeBalance := 0
		txInputs := []blockchain.TXInput{}
	LOOP:
		for shardId, utxoMap := range shardUtxoMap {
			for txId, vout2AmountMap := range utxoMap[senderAddressBytes] {
				txIdBytes, err := utils.Get32BytesFromString(txId)
				if err != nil {
					fmt.Println("Failed to parse txId")
					continue
				}
				for voutIndex, utxoAmount := range vout2AmountMap {
					cummulativeBalance += utxoAmount
					txIn := blockchain.NewTXInput(blockchain.NewOutPoint(&txIdBytes, voutIndex), senderAddressBytes, shardId)
					txInputs = append(txInputs, *txIn)
					if cummulativeBalance >= amount {
						break LOOP
					}
				}
			}
		}
		txout := blockchain.TXOutput{Amount: amount, Address: trimmedReceiverAddress, ShardID: uint32(math_rand.Intn(len(shardUtxoMap)))}

		txOutputs := []blockchain.TXOutput{txout}
		if cummulativeBalance > amount {
			changeTxOut := blockchain.TXOutput{Amount: cummulativeBalance - amount, Address: senderAddressBytes, ShardID: uint32(math_rand.Intn(len(shardUtxoMap)))}
			txOutputs = append(txOutputs, changeTxOut)
		}

		tx := blockchain.Transaction{ID: [32]byte{}, PublicKey: pki.GetBytesFromPublicKey(pki.GetPublicKeyFromScalar(senderPriKey)), TxInput: txInputs, TxOutput: txOutputs, Proofs: nil}
		tx.SetID() // TODO(RJ): figure out the correct way to set Tx ID.
		tx.Sign(senderPriKey)

		pubKey := crypto.Ed25519Curve.Point()
		err = pubKey.UnmarshalBinary(tx.PublicKey[:])
		if err != nil {
			fmt.Println("Failed to deserialize public key", "error", err)
		}

		ExecuteTransaction(tx, walletNode)
	default:
		flag.PrintDefaults()
		os.Exit(1)
	}
}

func ExecuteTransaction(tx blockchain.Transaction, walletNode *node.Node) error {
	if tx.IsCrossShard() {
		walletNode.Client.PendingCrossTxsMutex.Lock()
		walletNode.Client.PendingCrossTxs[tx.ID] = &tx
		walletNode.Client.PendingCrossTxsMutex.Unlock()

	}
	fmt.Println("Sending transaction...")

	msg := proto_node.ConstructTransactionListMessage([]*blockchain.Transaction{&tx})
	p2p.BroadcastMessage(*walletNode.Client.Leaders, msg)

	doneSignal := make(chan int)
	go func() {
		for true {
			if len(walletNode.Client.PendingCrossTxs) == 0 {
				doneSignal <- 0
				break
			}
		}
	}()

	select {
	case <-doneSignal:
		time.Sleep(100 * time.Millisecond)
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("Cross-shard tx timed out")
	}
}

func FetchUtxos(addresses [][20]byte, walletNode *node.Node) (map[uint32]blockchain.UtxoMap, error) {
	fmt.Println("Fetching account balance...")
	walletNode.Client.ShardUtxoMap = make(map[uint32]blockchain.UtxoMap)
	p2p.BroadcastMessage(*walletNode.Client.Leaders, proto_node.ConstructFetchUtxoMessage(*walletNode.ClientPeer, addresses))

	doneSignal := make(chan int)
	go func() {
		for true {
			if len(walletNode.Client.ShardUtxoMap) == len(*walletNode.Client.Leaders) {
				doneSignal <- 0
				break
			}
		}
	}()

	select {
	case <-doneSignal:
		return walletNode.Client.ShardUtxoMap, nil
	case <-time.After(3 * time.Second):
		return nil, errors.New("Utxo fetch timed out")
	}
}

func PrintUtxoBalance(shardUtxoMap map[uint32]blockchain.UtxoMap) {
	addressBalance := make(map[[20]byte]int)
	for _, utxoMap := range shardUtxoMap {
		for address, txHash2Vout2AmountMap := range utxoMap {
			for _, vout2AmountMap := range txHash2Vout2AmountMap {
				for _, amount := range vout2AmountMap {
					value, ok := addressBalance[address]
					if ok {
						addressBalance[address] = value + amount
					} else {
						addressBalance[address] = amount
					}
				}
			}
		}
	}
	for address, balance := range addressBalance {
		fmt.Printf("Address: {%x}\n", address)
		fmt.Printf("Balance: %d\n", balance)
	}
}

func ReadAddresses() [][20]byte {
	priKeys := ReadPrivateKeys()
	addresses := [][20]byte{}
	for _, key := range priKeys {
		addresses = append(addresses, pki.GetAddressFromPrivateKey(key))
	}
	return addresses
}

func StorePrivateKey(priKey []byte) {
	for _, address := range ReadAddresses() {
		if address == pki.GetAddressFromPrivateKey(crypto.Ed25519Curve.Scalar().SetBytes(priKey)) {
			fmt.Println("The key already exists in the keystore")
			return
		}
	}
	f, err := os.OpenFile("keystore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	if err != nil {
		panic("Failed to open keystore")
	}
	_, err = f.Write(priKey)

	if err != nil {
		panic("Failed to write to keystore")
	}
	f.Close()
}

func DeletePrivateKey() {
	ioutil.WriteFile("keystore", []byte{}, 0644)
}

func ReadPrivateKeys() []kyber.Scalar {
	keys, err := ioutil.ReadFile("keystore")
	if err != nil {
		return []kyber.Scalar{}
	}
	keyScalars := []kyber.Scalar{}
	for i := 0; i < len(keys); i += 32 {
		priKey := crypto.Ed25519Curve.Scalar()
		priKey.UnmarshalBinary(keys[i : i+32])
		keyScalars = append(keyScalars, priKey)
	}
	return keyScalars
}