// Copyright © 2018-2020 Satinderjit Singh.
//
// See the AUTHORS, DEVELOPER-AGREEMENT and LICENSE files at
// the top-level directory of this distribution for the individual copyright
// holder information and the developer policies on copyright and licensing.
//
// Unless otherwise agreed in a custom licensing agreement, no part of the
// kmdgo software, including this file may be copied, modified, propagated.
// or distributed except according to the terms contained in the LICENSE file
//
// Removal or modification of this copyright notice is prohibited.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/satindergrewal/kmdgo"
	r "gopkg.in/rethinkdb/rethinkdb-go.v6"
)

// session for rethink db
var session *r.Session

// Define appName type from kmdgo package
// Define appname variable. The name value must be the matching value of it's data directory name.
// Example Komodo's data directory is `komodo`, VerusCoin's data directory is `VRSC` and so on.
var appName kmdgo.AppType

// Rethink database name
var rDB string

func init() {
	var err error
	session, err = r.Connect(r.ConnectOpts{
		Address: "localhost:28015",
		// Database: rDB,
	})
	if err != nil {
		fmt.Println(err)
		return
	}
}

func main() {

	chainName := flag.String("chain", "VRSC", "Define appname variable. The name value must be the matching value of it's data directory name. Example Komodo's data directory is `komodo`, VerusCoin's data directory is `VRSC` and so on.")
	rDBName := flag.String("dbname", "vrsc", "Rethink database name")
	flag.Parse()
	// fmt.Println("chain:", *chainName)
	// fmt.Println("dbname:", *rDBName)
	appName = kmdgo.AppType(*chainName)
	rDB = *rDBName

	go networkInfoDB()
	go txAccountBlockTimeUpdate()

	// go checkSyncBlocksDB()
	go syncBlocksDB()
	go checkIfBlocksSynced()

	fmt.Scanln()
}

func round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

func toFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(round(num*output)) / output
}

func minimum(x, y float64) float64 {
	if x < y {
		return x
	}
	return y
}

func networkInfoDB() {
	// Keeps updating network info in network table every 200 milli seconds
	for {
		// Collect getinfo information
		_info, _ := appName.RPCResultMap("getinfo", []interface{}{})
		info := _info.(map[string]interface{})

		// Collect block hash of latest known block number
		blockHash, _ := appName.RPCResultMap("getblockhash", []interface{}{info["blocks"]})
		// fmt.Printf("Block Hash of %v: %v\n", info["blocks"], blockHash)

		// Collect network information
		_netInfo, _ := appName.RPCResultMap("getnetworkinfo", []interface{}{})
		netInfo := _netInfo.(map[string]interface{})
		// fmt.Println("Network Info: ", netInfo)

		// Collect network hashes per second data
		netHashPs, _ := appName.RPCResultMap("getnetworkhashps", []interface{}{120, -1})
		// fmt.Println("Network Hash: ", netHashPs)

		// Get information on total coinsupply and total funds residing in shielded info
		_supply, _ := appName.RPCResultMap("coinsupply", []interface{}{})
		supply := _supply.(map[string]interface{})
		// fmt.Println(supply)

		netInfoDB := map[string]interface{}{
			"blockHash":            blockHash,
			"blockNumber":          info["blocks"],
			"difficulty":           info["difficulty"],
			"hashrate":             int64(netHashPs.(float64)),
			"keypoolOldest":        info["keypoololdest"],
			"keypoolSize":          info["keypoolsize"],
			"isSyncComplete":       info["isSyncComplete"],
			"lastSynced":           info["lastSynced"],
			"name":                 rDB,
			"peerCount":            info["connections"],
			"protocolVersion":      info["protocolversion"],
			"relayFee":             info["relayfee"],
			"subVersion":           netInfo["subversion"],
			"coinSupply":           supply["total"],
			"zfunds":               supply["zfunds"],
			"version":              info["version"],
			"VRSCversion":          info["VRSCversion"],
			"notarized":            info["notarized"],
			"prevMoMheight":        info["prevMoMheight"],
			"notarizedhash":        info["notarizedhash"],
			"notarizedtxid":        info["notarizedtxid"],
			"notarizedtxid_height": info["notarizedtxid_height"],
			"KMDnotarized_height":  info["KMDnotarized_height"],
			"notarized_confirms":   info["notarized_confirms"],
			"premine":              info["premine"],
			"reward":               info["reward"],
			"halving":              info["halving"],
			"decay":                info["decay"],
			"endsubsidy":           info["endsubsidy"],
			"isreserve":            info["isreserve"],
			"veruspos":             info["veruspos"],
		}
		// fmt.Printf("%+v\n", netInfoDB)

		// Insert collected network information to network table, and if it gets conflicted with existing record update the existing record there
		err := r.DB(rDB).Table("network").Insert(netInfoDB, r.InsertOpts{Conflict: networkMerge}).Exec(session)
		if err != nil {
			log.Printf("Error writing network info to DB: %v", err)
		}
		sleepSeconds := 10
		fmt.Printf("Updated Network Info. Will update again in %v seconds...\n", sleepSeconds)
		time.Sleep(time.Duration(sleepSeconds) * time.Second)
	}
}

func txAccountBlockTimeUpdate() {
	for {
		// Collect information about total Transactions in the database
		res, err := r.DB(rDB).Table("transactions").Count().Run(session)
		if err != nil {
			log.Printf("Error collecting total count of transactions in database: %v", err)
		}
		var totalTx int
		res.One(&totalTx)
		res.Close()
		// fmt.Println("totalTx -", totalTx)

		// Collect how many total accounts are found and recorded in the database
		res, err = r.DB(rDB).Table("accounts").Count().Run(session)
		if err != nil {
			log.Printf("Error collecting total count of accounts in database: %v", err)
		}
		var totalAccounts int
		res.One(&totalAccounts)
		res.Close()
		// fmt.Println("totalAccounts -", totalAccounts)

		// Collect how many total identities are found and recorded in the database
		res, err = r.DB(rDB).Table("identities").Count().Run(session)
		if err != nil {
			log.Printf("Error collecting total count of accounts in database: %v", err)
		}
		var totalIdentities int
		res.One(&totalIdentities)
		res.Close()
		// fmt.Println("totalIdentities -", totalIdentities)

		// Calculate average block generation time by taking last 120 block's time
		res, err = r.DB(rDB).Table("blocks").OrderBy(r.OrderByOpts{Index: r.Desc("height")}).Limit(120).Filter(
			func(row r.Term) interface{} { return row.HasFields("time") }).Map(
			func(row r.Term) interface{} { return row.Field("time") }).Run(session)
		if err != nil {
			log.Printf("Error collecting time for last 120 blocks: %v", err)
		}
		var collective120BlockTimes []float64
		res.All(&collective120BlockTimes)
		res.Close()
		// fmt.Println(collective120BlockTimes)
		totalSumOfTime := float64(0)
		for _, time := range collective120BlockTimes {
			// Add all found block times together to make single sum of total time
			totalSumOfTime += time
			// fmt.Println(i, " - ", time)
			// fmt.Println(time)
		}
		// fmt.Println(totalSumOfTime)
		// Average block time = (Total sum of all last 120 block times) / (Total number of blocks = 120)
		averageBlockTime := totalSumOfTime / 120
		// fmt.Println("averageBlockTime -", averageBlockTime)

		err = r.DB(rDB).Table("network").Get(rDB).Update(map[string]interface{}{
			"transactions":     totalTx,
			"accounts":         totalAccounts,
			"averageBlockTime": averageBlockTime,
			"identities":       totalIdentities,
		}).Exec(session)
		if err != nil {
			log.Printf("Error updating network stats: %v", err)
		}
		res.Close()
		sleepSeconds := 60
		fmt.Printf("Updated Total Transactions, Total Accounts and Average block time info. Will update again in %v seconds...\n", sleepSeconds)
		time.Sleep(time.Duration(sleepSeconds) * time.Second)
	}
}

func checkIfBlocksSynced() {
	// Get a realtime live feed of network table update changes
	res, err := r.DB(rDB).Table("network").Changes().Run(session)
	var value map[string]interface{}
	if err != nil {
		log.Fatalln(err)
	}

	for res.Next(&value) {
		// filter out value of isSyncComplete and check if it's true, which indicates blocks sync is complete with the database
		if value["new_val"].(map[string]interface{})["isSyncComplete"] == true {
			// if isSyncComplete is true, compare if the latest block synced by the daemon and reflected by getinfo is matching the last synced block to the database
			if value["new_val"].(map[string]interface{})["blockNumber"] != value["new_val"].(map[string]interface{})["lastSynced"] {
				// If last synced block in database and the blocks from getinfo doesn't match, change the status of isSyncComplete to false
				err = r.DB(rDB).Table("network").Get(rDB).Update(map[string]interface{}{
					"isSyncComplete": false,
				}).Exec(session)
				if err != nil {
					log.Panicf("Failed to write sync info to DB: %v", err)
				}
				// and also trigger syncBlocksDB function to check and update database blocks to sync with the blockchain
				syncBlocksDB()
			}
		}
	}
}

func syncBlocksDB() {
	var netRow map[string]interface{}
	cursor, err := r.DB(rDB).Table("network").Get(rDB).Run(session)
	if err != nil {
		log.Panicf("Failed to get network info from DB: %v", err)
	}
	cursor.One(&netRow)
	cursor.Close()

	var lastSynced, latestBlock uint64

	// fmt.Println("netRow["lastSynced"] -", netRow["lastSynced"])
	// fmt.Println("netRow["blockNumber"] -", netRow["blockNumber"])

	if netRow["lastSynced"] != nil && netRow["lastSynced"] != 0 {
		lastSynced = uint64(netRow["lastSynced"].(float64)) + 1
		// lastSynced = 52391
	} else {
		// lastSynced = 52391
	}
	if netRow["blockNumber"] != nil && netRow["blockNumber"] != 0 {
		latestBlock = uint64(netRow["blockNumber"].(float64))
	} else {
		_info, _ := appName.RPCResultMap("getinfo", []interface{}{})
		latestBlock = uint64(_info.(map[string]interface{})["blocks"].(float64))
	}

	// fmt.Println("lastSynced -", lastSynced)
	// fmt.Println("latestBlock -", latestBlock)

	for blockNum := lastSynced; blockNum <= latestBlock; blockNum++ {
		percentSyncDone := float64(float64(blockNum)/float64(latestBlock)) * 100
		pStr := fmt.Sprintf("%.2f", percentSyncDone) + "%"
		fmt.Println("Last synced - ", blockNum, "| Blocks remaining - ", latestBlock-blockNum, "| Percent Done - ", pStr)

		// Collect block details using block number
		_blockDetails, _ := appName.RPCResultMap("getblock", []interface{}{strconv.FormatUint(blockNum, 10), 2})
		blockDetails := _blockDetails.(map[string]interface{})
		blockGenTime := float64(0)

		if blockNum > 0 {
			// Get previous block info
			_prevBlockDetails, _ := appName.RPCResultMap("getblock", []interface{}{strconv.FormatUint(blockNum-1, 10)})
			prevBlockDetails := _prevBlockDetails.(map[string]interface{})
			blockGenTime = blockDetails["time"].(float64) - prevBlockDetails["time"].(float64)
		}

		// To store the list of all transaction IDs from the block
		var txidList []string

		// To store and get the address of miner, block producer from coinbase output, the first Address from the vout array
		var _minerAddress string

		// Get array of all transactions from block output and store it to a variable
		txns := blockDetails["tx"].([]interface{})

		// Let's work with each tranaction in that array
		for txIndex, _txid := range txns {
			// fmt.Println(i, " - ", v.Txid)
			txData := _txid.(map[string]interface{})
			txidList = append(txidList, txData["txid"].(string))

			vOutData := txData["vout"].([]interface{})
			if txIndex == 0 {
				_minerAddress = vOutData[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})["addresses"].([]interface{})[0].(string)
				// forward data to accounts update function to add/update miner address/account details
				addMinerAccount(_txid, blockDetails)
				// fmt.Scanln()
			}
			// forward data to identity update function to add/update identities details
			addUpdateIdentity(vOutData, blockDetails)

			// Process transactions from block and insert it to database table
			// insertTxDB(txIndex, _txid, blockDetails)
			retrievedVout, txSenders := insertTxDB(txIndex, _txid, blockDetails)
			// if retrievedVout != nil {
			// 	fmt.Println("retrievedVout -", retrievedVout)
			// 	fmt.Println("txSenders - ", txSenders)
			// 	fmt.Scanln()
			// }

			// Update sent values/balances in accounts addresses
			updateSentBalances(txData, retrievedVout, blockDetails, txSenders)

			// Update recieved values/balances in accounts addresses
			updateRecvBalances(txData, retrievedVout, blockDetails, txSenders)
		}

		blockDBItem := map[string]interface{}{
			"bits":         blockDetails["bits"],
			"chainWork":    blockDetails["chainwork"],
			"difficulty":   blockDetails["difficulty"],
			"hash":         blockDetails["hash"],
			"height":       blockDetails["height"],
			"merkleRoot":   blockDetails["merkleroot"],
			"nonce":        blockDetails["nonce"],
			"size":         blockDetails["size"],
			"solution":     blockDetails["solution"],
			"time":         blockGenTime,
			"timestamp":    blockDetails["time"],
			"transactions": blockDetails["tx"],
			"version":      blockDetails["version"],
		}

		if _minerAddress != "" {
			blockDBItem["miner"] = _minerAddress
		}

		if blockDetails["previousblockhash"] != nil {
			blockDBItem["previousBlock"] = blockDetails["previousblockhash"]
		}
		if blockDetails["nextblockhash"] != nil {
			blockDBItem["nextBlock"] = blockDetails["nextblockhash"]
		}

		// Insert new block to to the database
		err = r.DB(rDB).Table("blocks").Insert(blockDBItem, r.InsertOpts{Conflict: "update"}).Exec(session)
		if err != nil {
			log.Panicf("Failed to write block info to DB: %v", err)
		}
		log.Printf("New block added Hash: %s | Block Number: %v", blockDetails["hash"], blockNum)

		// Update last synced block once remaining blocks are all synced and matched with the blockchain results
		err = r.DB(rDB).Table("network").Get(rDB).Update(map[string]interface{}{"lastSynced": blockNum}).Exec(session)
		if err != nil {
			log.Panicf("Failed to write sync info to DB: %v", err)
		}
	}
	// block sync status update in network table
	err = r.DB(rDB).Table("network").Get(rDB).Update(map[string]interface{}{
		"isSyncComplete": true,
	}).Exec(session)
	if err != nil {
		log.Panicf("Failed to write sync info to DB: %v", err)
	}
	fmt.Println("blocks sync completed!")
}

// add/update block miner's address to accounts table
func addMinerAccount(txidData interface{}, block map[string]interface{}) {
	// fmt.Printf("%T\n", txidData)
	// fmt.Println(txidData.Vout[0].ScriptPubKey)
	txData := txidData.(map[string]interface{})
	vOutData := txData["vout"].([]interface{})
	_minerAddress := vOutData[0].(map[string]interface{})["scriptPubKey"].(map[string]interface{})["addresses"].([]interface{})[0].(string)
	_account := map[string]interface{}{
		"address":    _minerAddress,
		"balance":    0,
		"firstSeen":  int64(block["time"].(float64)),
		"lastSeen":   int64(block["time"].(float64)),
		"mined":      []string{block["hash"].(string)},
		"minedCount": 1,
		"recv":       []string{},
		"recvCount":  0,
		"sent":       []string{},
		"sentCount":  0,
		"totalRecv":  0,
		"totalSent":  0,
	}
	err := r.DB(rDB).Table("accounts").Insert(_account, r.InsertOpts{Conflict: accountMerge}).Exec(session)
	if err != nil {
		log.Panicf("Failed to write transaction info to DB: %v", err)
	}
	log.Printf("Updated account %s", _minerAddress)
}

func updateSentBalances(txData, retrievedVout, block map[string]interface{}, txSenders []interface{}) {
	vInData := txData["vin"].([]interface{})

	sent := make(map[string]bool)

	// iterate through all INPUT/vin objects to process and update accounts/address in "accounts" table
	for index, _vInObj := range vInData {
		vInObj := _vInObj.(map[string]interface{})

		// we got this sender's address from the previous transaction, which was OUTPUT in previous txid
		// this address was collected from insertTxDB() function which processes all the transactions IDs and it's data in the block
		senderAddr := txSenders[index]
		// if there is no address collected from this vin, skip to process the next vin
		if senderAddr == nil {
			continue
		}
		// if his vin has no txid and the previous OUTPUT linked to this INPUT also doesn't exists, skip to process the next vin
		if vInObj["txid"] != nil && retrievedVout == nil {
			continue
		}
		// for some reason if the value in this INPUT/vin is nil, use value from previous OUTPUT collected from insertTxDB() function
		if vInObj["value"] == nil { // why
			log.Printf("Value was nil: %v", vInObj)
			vInObj["value"] = retrievedVout["value"]
		}
		// make a temporary array to store txids
		sentt := make([]interface{}, 0)
		// check and make the current address to be processed, if it's not already processed,
		// and append the txid for this address into a temporary array variable
		if sent[senderAddr.(string)] != true {
			sentt = append(sentt, txData["txid"])
			sent[senderAddr.(string)] = true
		}
		// insert/update account/address record in accounts table.
		// if the address in the table already exists, the conflict will retrun the updated JSON
		// object with data merged from old and new data collected in this fuction and update the address record in database table.
		err := r.DB(rDB).Table("accounts").Insert(map[string]interface{}{
			"address":    senderAddr,
			"firstSeen":  block["time"],
			"lastSeen":   block["time"],
			"balance":    toFixed(-1.0*vInObj["value"].(float64), 8),
			"totalSent":  toFixed(vInObj["value"].(float64), 8),
			"totalRecv":  0,
			"minedCount": 0,
			"recvCount":  0,
			"sentCount":  1,
			"mined":      []interface{}{},
			"sent":       sentt,
			"recv":       []interface{}{},
		}, r.InsertOpts{Conflict: accountMerge}).Exec(session)
		if err != nil {
			log.Panicf("Failed to write transaction info to DB: %v", err)
		}
		log.Printf("Updated account %s", senderAddr)
	}
}

func updateRecvBalances(txData, retrievedVout, block map[string]interface{}, txSenders []interface{}) {
	vOutData := txData["vout"].([]interface{})
	for _, vOutObj := range vOutData {
		// TODO: Need to make seperate code to manage such tyep of vout transactions,
		// like parsing and storing data about verus currencies
		scriptPubKey := vOutObj.(map[string]interface{})["scriptPubKey"].(map[string]interface{})
		vOutValue := toFixed(vOutObj.(map[string]interface{})["value"].(float64), 8)

		if scriptPubKey["reservetransfer"] != nil {
			// TODO: add another function to store reservetransfter vouts to a dedicated table
			// for now skip "reservetransfer" vout to process next vout
			continue
		}

		if scriptPubKey["spendableoutput"] != nil && scriptPubKey["spendableoutput"].(bool) == false {
			// if current vout has "spendableoutput = false", this vout is not spendable
			// so skip this on and process the next one??
			// I'm honestly not sure if I'm doing it right :(
			continue
		}

		// if there's a spent information (spentTxId, spentIndex, spentHeight) found in this vout, also add this to the sent side of data
		if vOutObj.(map[string]interface{})["spentTxId"] != nil {
			// vOutValue = vOutObj.(map[string]interface{})["value"].(float64) - vOutObj.(map[string]interface{})["value"].(float64)
			// vOutValue = toFixed(-1.0*vOutObj.(map[string]interface{})["value"].(float64), 8)
			// spentIndex := vOutObj.(map[string]interface{})["spentIndex"]
			// spentHeight := vOutObj.(map[string]interface{})["spentHeight"]

			// collect spent txid to a variable from this vout to add to "sent" side of transactions for this address
			spentTxID := vOutObj.(map[string]interface{})["spentTxId"]
			// take the first address as sender address from "addresses" array from vout
			senderAddr := scriptPubKey["addresses"].([]interface{})[0].(string)
			// update accounts table with balance, totalsent, total sent count, and add the spent txid to sent side of an address.
			// this insert record to table command if conflicts with the existing account in that table,
			// it will just merge/update that account's details
			err := r.DB(rDB).Table("accounts").Insert(map[string]interface{}{
				"address":    senderAddr,
				"firstSeen":  block["time"],
				"lastSeen":   block["time"],
				"balance":    toFixed(-1.0*vOutObj.(map[string]interface{})["value"].(float64), 8),
				"totalSent":  toFixed(vOutObj.(map[string]interface{})["value"].(float64), 8),
				"totalRecv":  0,
				"minedCount": 0,
				"recvCount":  0,
				"sentCount":  1,
				"mined":      []interface{}{},
				"sent":       []interface{}{spentTxID},
				"recv":       []interface{}{},
			}, r.InsertOpts{Conflict: accountMerge}).Exec(session)
			if err != nil {
				log.Panicf("Failed to write transaction info to DB: %v", err)
			}
			log.Printf("Updated account %s", senderAddr)
		}

		// if scriptPubKey["spendableoutput"] != nil && scriptPubKey["spendableoutput"].(bool) == false {
		// 	if vOutObj.(map[string]interface{})["spentTxId"] != nil {
		// 		// vOutValue = toFixed(0.0, 8)
		// 		vOutValue = toFixed(-1.0*vOutObj.(map[string]interface{})["value"].(float64), 8)
		// 	}
		// } else {
		// 	if vOutObj.(map[string]interface{})["spentTxId"] != nil {
		// 		vOutValue = toFixed(0.0, 8)
		// 	}
		// }

		// if "spendableoutput = true", it means the valeu/amount in this vout is spendable.
		// add this like a normal balance, total recieved, total recieved counts, and txid to the list of recived txids.
		if scriptPubKey["spendableoutput"] != nil && scriptPubKey["spendableoutput"].(bool) == true {
			vOutValue = toFixed(vOutObj.(map[string]interface{})["value"].(float64), 8)
		}
		// if there is no "addresses" JSON key in vout, just skip to process the next vout data
		if scriptPubKey["addresses"] == nil {
			continue
		}
		// add/update the collected data for account/address in accounts table for each address in the "addresses" array
		for _, addr := range scriptPubKey["addresses"].([]interface{}) {
			err := r.DB(rDB).Table("accounts").Insert(map[string]interface{}{
				"address":    addr.(string),
				"firstSeen":  block["time"],
				"lastSeen":   block["time"],
				"balance":    vOutValue,
				"totalSent":  0,
				"totalRecv":  vOutValue,
				"minedCount": 0,
				"recvCount":  1,
				"sentCount":  0,
				"mined":      []interface{}{},
				"sent":       []interface{}{},
				"recv":       []interface{}{txData["txid"]},
			}, r.InsertOpts{Conflict: accountMerge}).Exec(session)
			if err != nil {
				log.Panicf("Failed to write transaction info to DB: %v", err)
			}
			log.Printf("Updated account %s", addr)
		}
	}
}

// Insert/Update Identity table
func addUpdateIdentity(vOutData []interface{}, block map[string]interface{}) {
	// txData := txidData.(map[string]interface{})
	// vOutData := txData["vout"].([]interface{})
	if len(vOutData) != 0 {
		for _, voutv := range vOutData {
			scriptPubKey := voutv.(map[string]interface{})["scriptPubKey"].(map[string]interface{})
			if scriptPubKey["identityprimary"] != nil {
				identity := scriptPubKey["identityprimary"].(map[string]interface{})
				// fmt.Println("Identity found!")
				// fmt.Println(identity)
				err := r.DB(rDB).Table("identities").Insert(map[string]interface{}{
					"version":             identity["version"],
					"flags":               identity["flags"],
					"primaryaddresses":    identity["primaryaddresses"],
					"minimumsignatures":   identity["minimumsignatures"],
					"identityaddress":     identity["identityaddress"],
					"parent":              identity["parent"],
					"name":                identity["name"],
					"contentmap":          identity["contentmap"],
					"revocationauthority": identity["revocationauthority"],
					"recoveryauthority":   identity["recoveryauthority"],
					"privateaddress":      identity["privateaddress"],
					"timelock":            identity["timelock"],
					"firstSeen":           int64(block["time"].(float64)),
					"lastSeen":            int64(block["time"].(float64)),
					"blockheight":         block["height"],
					"txid":                block["hash"],
					"vout":                voutv.(map[string]interface{})["n"],
				}, r.InsertOpts{Conflict: identityMerge}).Exec(session)
				if err != nil {
					log.Panicf("Failed to write identity info to DB: %v", err)
				}
				log.Printf("Updated identity %s", identity["name"])
			}
		}
	}
}

func insertTxDB(txIndex int, txidData interface{}, block map[string]interface{}) (map[string]interface{}, []interface{}) {

	var retrievedVout map[string]interface{}

	// To identify and store the type of transaction
	var txType string
	// To identify and store if the transaction has shielded transaction values
	var isShielded bool

	txData := txidData.(map[string]interface{})
	vInData := txData["vin"].([]interface{})
	vOutData := txData["vout"].([]interface{})
	vJoinSplit := txData["vjoinsplit"].([]interface{})

	if len(vInData) == 0 {
		// In case of shielded transaction where the tranaction is coming from private address
		// and going out to a transparent address, it will show vin array with 0 data objects.
		// If that's the case, we mark this tranaction as value transfer, and set is shielded to true.
		txType = "valueTransfer"
		isShielded = true
	} else if vInData[0].(map[string]interface{})["coinbase"] != nil {
		// If the first vin object has "coinbase" key in it, that transaction is marked as miner's reward
		txType = "minerReward"
		isShielded = false
	} else {
		txType = "valueTransfer"
		if len(vJoinSplit) == 0 {
			isShielded = false
		} else {
			isShielded = true
		}
	}
	var shieldedValue1, shieldedValue2, inputValue, vpubOld, vpubNew, totalvOutValue, txFee float64
	totalvOutValue = 0

	// for storing shielded values in intput side
	shieldedValue1 = 0
	// for storing shielded values in output side
	shieldedValue2 = 0

	txFee = 0
	inputValue = 0
	for _, vOutObj := range vOutData {
		totalvOutValue += vOutObj.(map[string]interface{})["value"].(float64)
	}
	// if len(vJoinSplit) != 0 {
	// 	// fmt.Scanln()
	// }

	// Calculating shielded transactions value going in/out of transaction
	// For reference read VJoinSplit part from here: https://killiandavitt.me/zcash_data_mining.pdf
	// Another refernce link regarding vpub_old/vpub_new: https://github.com/zcash/zcash/issues/3428#issuecomment-408828237
	// 	"vpub_old" : x.xxx,         (numeric) public input value in ZEC
	// 	"vpub_new" : x.xxx,         (numeric) public output value in ZEC
	for _, joinsplit := range vJoinSplit {
		oldV := joinsplit.(map[string]interface{})["vpub_old"].(float64)
		newV := joinsplit.(map[string]interface{})["vpub_new"].(float64)
		vpubOld += oldV
		vpubNew += newV
		diff := oldV - newV
		if diff > 0 {
			// It means public -> shielded transaction is done
			shieldedValue2 += diff
		}
		if diff < 0 {
			// it means shielded -> public transaction is done
			inputValue -= diff
		}
	}
	shieldedValue1 = vpubOld - vpubNew
	if shieldedValue1 < 0 {
		// just removing the "-" from the number
		shieldedValue1 = -shieldedValue1
	}
	txSenders := make([]interface{}, len(vInData))
	inputValue2 := float64(0)
	for index, _vInObj := range vInData {
		vInObj := _vInObj.(map[string]interface{})
		if vInObj["txid"] != nil {
			// fmt.Println(`txData["txid"] -- `, txData["txid"])
			// fmt.Printf("vInObj -- %v\n", vInObj)
			// fmt.Scanln()

			// Every input in bitcoin blockchain uses output from previous transaction.
			// For this input transaction, get the information from previous transactions output
			// and get "transaction amount" and "sent from" address details from that output, along with the whole vout
			// to pass to next section of code to process that for calculating balances for accounts/addresses
			_rawtx, _ := appName.RPCResultMap("getrawtransaction", []interface{}{vInObj["txid"], 1})
			rawtx := _rawtx.(map[string]interface{})
			rawTxvOutData := rawtx["vout"].([]interface{})
			if rawtx == nil || rawtx["vout"] == nil {
				continue
			}
			rawtxVoutIndex := int(vInObj["vout"].(float64))
			if rawtxVoutIndex < len(rawTxvOutData) {
				prevTxIDvout := rawTxvOutData[rawtxVoutIndex].(map[string]interface{})
				retrievedVout = prevTxIDvout
				// vInObj["retrievedVout"] = out
				inputValue += prevTxIDvout["value"].(float64)
				inputValue2 += prevTxIDvout["value"].(float64)
				// store "sent from addresses" for current index of vin transaction
				txSenders[index] = prevTxIDvout["scriptPubKey"].(map[string]interface{})["addresses"].([]interface{})[0]
			} else {
				log.Println("Unable to retrieve vout")
			}
		}
	}
	if txType == "valueTransfer" {
		txFee = inputValue - (totalvOutValue + shieldedValue2)
	}
	if txFee < 0 {
		txFee = 0
	} // could be more intelligent
	txFee = toFixed(txFee, 8)
	outputValue := totalvOutValue
	if inputValue2 > totalvOutValue {
		totalvOutValue = inputValue2
	}
	totalvOutValue = toFixed(totalvOutValue, 8)
	shieldedValue1 = toFixed(shieldedValue1, 8)
	info := map[string]interface{}{
		"hash":          txData["txid"],
		"fee":           txFee,
		"type":          txType,
		"shielded":      isShielded,
		"index":         txIndex,
		"blockHash":     block["hash"],
		"blockHeight":   block["height"],
		"version":       txData["version"],
		"lockTime":      txData["locktime"],
		"timestamp":     block["time"],
		"vin":           txData["vin"],
		"vout":          txData["vout"],
		"vjoinsplit":    txData["vjoinsplit"],
		"overwintered":  txData["overwintered"],
		"value":         totalvOutValue,
		"outputValue":   outputValue,
		"shieldedValue": shieldedValue1,
	}
	// fmt.Println("info -- ", info)
	err := r.DB(rDB).Table("transactions").Insert(info, r.InsertOpts{Conflict: "update"}).Exec(session)
	if err != nil {
		log.Panicf("Failed to write transaction info to DB: %v", err)
	}
	// log.Printf("Wrote tx %s to DB", txData["txid"])

	return retrievedVout, txSenders
}

func accountMerge(key r.Term, oldDoc r.Term, newDoc r.Term) interface{} {
	return map[string]interface{}{
		"address":    oldDoc.Field("address"),
		"firstSeen":  oldDoc.Field("firstSeen"),
		"lastSeen":   newDoc.Field("lastSeen"),
		"balance":    oldDoc.Field("balance").Add(newDoc.Field("balance")),
		"totalSent":  oldDoc.Field("totalSent").Add(newDoc.Field("totalSent")),
		"totalRecv":  oldDoc.Field("totalRecv").Add(newDoc.Field("totalRecv")),
		"minedCount": oldDoc.Field("minedCount").Add(newDoc.Field("minedCount")),
		"recvCount":  oldDoc.Field("recvCount").Add(newDoc.Field("recvCount")),
		"sentCount":  oldDoc.Field("sentCount").Add(newDoc.Field("sentCount")),
		"mined":      newDoc.Field("mined").Default([]interface{}{}).Add(oldDoc.Field("mined").Default([]interface{}{})), // .SetUnion([]interface{}{}),
		"recv":       newDoc.Field("recv").Default([]interface{}{}).Add(oldDoc.Field("recv").Default([]interface{}{})),   //.SetUnion([]interface{}{}),
		"sent":       newDoc.Field("sent").Default([]interface{}{}).Add(oldDoc.Field("sent").Default([]interface{}{})),   //.SetUnion([]interface{}{}),
	}
}

func identityMerge(key r.Term, oldDoc r.Term, newDoc r.Term) interface{} {
	return map[string]interface{}{
		"version":             oldDoc.Field("version"),
		"flags":               newDoc.Field("flags"),
		"primaryaddresses":    newDoc.Field("primaryaddresses"),
		"minimumsignatures":   newDoc.Field("minimumsignatures"),
		"identityaddress":     newDoc.Field("identityaddress"),
		"parent":              newDoc.Field("identityaddress"),
		"name":                oldDoc.Field("name"),
		"contentmap":          newDoc.Field("contentmap"),
		"revocationauthority": newDoc.Field("revocationauthority"),
		"recoveryauthority":   newDoc.Field("recoveryauthority"),
		"privateaddress":      newDoc.Field("privateaddress"),
		"timelock":            newDoc.Field("timelock"),
		"firstSeen":           oldDoc.Field("firstSeen"),
		"lastSeen":            newDoc.Field("lastSeen"),
		"blockheight":         newDoc.Field("blockheight"),
		"txid":                newDoc.Field("txid"),
		"vout":                newDoc.Field("vout"),
	}
}

func networkMerge(key r.Term, oldDoc r.Term, newDoc r.Term) interface{} {
	return map[string]interface{}{
		"accounts":             oldDoc.Field("accounts"),
		"identities":           oldDoc.Field("identities"),
		"blockHash":            newDoc.Field("blockHash"),
		"blockNumber":          newDoc.Field("blockNumber"),
		"difficulty":           newDoc.Field("difficulty"),
		"hashrate":             newDoc.Field("hashrate"),
		"keypoolOldest":        newDoc.Field("keypoolOldest"),
		"keypoolSize":          newDoc.Field("keypoolSize"),
		"isSyncComplete":       oldDoc.Field("isSyncComplete"),
		"lastSynced":           oldDoc.Field("lastSynced"),
		"averageBlockTime":     oldDoc.Field("averageBlockTime"),
		"name":                 newDoc.Field("name"),
		"peerCount":            newDoc.Field("peerCount"),
		"protocolVersion":      newDoc.Field("protocolVersion"),
		"relayFee":             newDoc.Field("relayFee"),
		"subVersion":           newDoc.Field("subVersion"),
		"coinSupply":           newDoc.Field("coinSupply"),
		"zfunds":               newDoc.Field("zfunds"),
		"transactions":         oldDoc.Field("transactions"),
		"version":              newDoc.Field("version"),
		"VRSCversion":          newDoc.Field("VRSCversion"),
		"notarized":            newDoc.Field("notarized"),
		"prevMoMheight":        newDoc.Field("prevMoMheight"),
		"notarizedhash":        newDoc.Field("notarizedhash"),
		"notarizedtxid":        newDoc.Field("notarizedtxid"),
		"notarizedtxid_height": newDoc.Field("notarizedtxid_height"),
		"KMDnotarized_height":  newDoc.Field("KMDnotarized_height"),
		"notarized_confirms":   newDoc.Field("notarized_confirms"),
		"premine":              newDoc.Field("premine"),
		"reward":               newDoc.Field("reward"),
		"halving":              newDoc.Field("halving"),
		"decay":                newDoc.Field("decay"),
		"endsubsidy":           newDoc.Field("endsubsidy"),
		"isreserve":            newDoc.Field("isreserve"),
		"veruspos":             newDoc.Field("veruspos"),
	}
}

func printStr(v string) {
	fmt.Println(v)
}

func printObj(v interface{}) {
	vBytes, _ := json.Marshal(v)
	fmt.Println(string(vBytes))
}

// RethinkDB data explorer commmands for creating database tables and test queries
//
// r.dbCreate('vrscdb')
// r.db('vrscdb').tableDrop('blocks')
// r.db('vrscdb').tableCreate('blocks', {primaryKey: 'hash'})
// r.db('vrscdb').tableCreate('blocks', {primaryKey: 'block_db_id'})
// r.db('vrscdb').tableCreate('blocks')
// r.db('vrscdb').table('blocks').count()
// r.db('vrscdb').table('blocks')
// r.db('vrscdb').table('blocks').indexCreate('height')
// r.db('vrscdb').table('blocks').indexCreate('timestamp')
// r.db('vrscdb').table('blocks').indexCreate('time')
// r.db('vrscdb').table('blocks').indexCreate('difficulty')
// r.db('vrscdb').table('blocks').indexCreate('miner')
// r.db('vrscdb').table('blocks').indexCreate('transactions', lambda x: x['transactions'].count())
// r.db('vrscdb').tableCreate('transactions', {primaryKey: 'hash'})
// r.db('vrscdb').table('transactions').indexCreate('value')
// r.db('vrscdb').table('transactions').indexCreate('timestamp')
// r.db('vrscdb').table('transactions').indexCreate('blockHeight')
// r.db('vrscdb').table('transactions').indexCreate('blockHash')
// r.db('vrscdb').table('transactions').indexCreate('shieldedValue')
// r.db('vrscdb').tableCreate('accounts', {primaryKey: 'address'})
// r.db('vrscdb').table('accounts').indexCreate('lastSeen')
// r.db('vrscdb').table('accounts').indexCreate('firstSeen')
// r.db('vrscdb').table('accounts').indexCreate('balance')
// r.db('vrscdb').table('blocks').filter({height: '111750'})
// r.db('vrscdb').table('blocks').indexList()
// r.db('vrscdb').tableCreate('network', {primaryKey: 'name'})
// r.db('vrscdb').tableCreate('logs')
// r.db('vrscdb').tableCreate('stats', {primaryKey: 'name'})
// r.db('vrscdb').tableCreate('identity', {primaryKey: 'name'})
// r.db('vrscdb').table('identity').indexCreate('identityaddress')
// r.db('vrscdb').table('identity').indexCreate('parent')
// r.db('vrscdb').table('identity').indexCreate('privateaddress')

// search by block hash
// r.db('vrscdb').table('blocks').filter({hash: 'fa2d5e5f5fb42af9d6343fa93bbc776761341fd754a7e078004132bcd8403dd2'})

// search by block number
// r.db('vrscdb').table('blocks').getAll(111750, {index:'height'})

// get live feed of last synced block from network table
// r.db('vrscdb').table('network').pluck('lastSynced').changes()

// r.db('vrscdb').table('transactions').getAll('d63d0de99f9cc2558f484ac44cfe61b3dea0482ca1550c0b6fccf7f2c8c399df', {index:'hash'})

// get array/list of names from identities table
// r.db('vrsctest').table('identities').getField('name')

// search identities with a given matching string
// r.db('vrsctest').table('identities').filter(function(doc){return doc('name').match("^a")}).getField('name')
