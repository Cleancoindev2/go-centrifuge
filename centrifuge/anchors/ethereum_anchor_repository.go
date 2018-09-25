package anchors

import (
	"context"
	"math/big"

	"github.com/centrifuge/go-centrifuge/centrifuge/identity"
	"github.com/centrifuge/go-centrifuge/centrifuge/queue"
	"github.com/centrifuge/gocelery"

	"github.com/centrifuge/go-centrifuge/centrifuge/config"
	"github.com/centrifuge/go-centrifuge/centrifuge/ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/go-errors/errors"
)

type EthereumAnchorRepository struct {
}

type AnchorRepositoryContract interface {

	//transactions
	PreCommit(opts *bind.TransactOpts, anchorID *big.Int, signingRoot [32]byte, centrifugeId *big.Int, signature []byte, expirationBlock *big.Int) (*types.Transaction, error)
	Commit(opts *bind.TransactOpts, _anchorID *big.Int, _documentRoot [32]byte, _centrifugeId *big.Int, _documentProofs [][32]byte, _signature []byte) (*types.Transaction, error)
}
type WatchAnchorPreCommitted interface {
	//event name: AnchorPreCommitted
	WatchAnchorPreCommitted(opts *bind.WatchOpts, sink chan<- *EthereumAnchorRepositoryContractAnchorPreCommitted,
		from []common.Address, anchorID []*big.Int) (event.Subscription, error)
}

type WatchAnchorCommitted interface {
	//event name: AnchorCommitted
	WatchAnchorCommitted(opts *bind.WatchOpts, sink chan<- *EthereumAnchorRepositoryContractAnchorCommitted,
		from []common.Address, anchorID []*big.Int, centrifugeId []*big.Int) (event.Subscription, error)
}

//PreCommitAnchor will call the transaction PreCommit on the smart contract
func (ethRepository *EthereumAnchorRepository) PreCommitAnchor(anchorID AnchorID, signingRoot DocRoot, centrifugeId identity.CentID, signature []byte, expirationBlock *big.Int) (confirmations <-chan *WatchPreCommit, err error) {

	//TODO check if parameters are valid
	ethRepositoryContract, _ := getRepositoryContract()
	opts, err := ethereum.GetConnection().GetTxOpts(config.Config.GetEthereumDefaultAccountName())
	if err != nil {
		return
	}
	preCommitData := NewPreCommitData(anchorID, signingRoot, centrifugeId, signature, expirationBlock)
	if err != nil {
		return
	}

	confirmations, err = setUpPreCommitEventListener(ethRepositoryContract, opts.From, preCommitData)
	if err != nil {
		wError := errors.Wrap(err, 1)
		log.Errorf("Failed to set up event listener for pre-commit transaction [id: %x, signingRoot: %x, SchemaVersion:%v]: %v",
			preCommitData.AnchorID, preCommitData.SigningRoot, preCommitData.SchemaVersion, wError)
		return
	}

	err = sendPreCommitTransaction(ethRepositoryContract, opts, preCommitData)
	if err != nil {
		wError := errors.Wrap(err, 1)
		log.Errorf("Failed to send Ethereum pre-commit transaction [id: %x, signingRoot: %x, SchemaVersion:%v]: %v",
			preCommitData.AnchorID, preCommitData.SigningRoot, preCommitData.SchemaVersion, wError)
		return
	}
	return confirmations, err
}

// sendPreCommitTransaction sends the actual transaction to the ethereum node
func sendPreCommitTransaction(contract AnchorRepositoryContract, opts *bind.TransactOpts, preCommitData *PreCommitData) (err error) {

	//preparation of data in specific types for the call to Ethereum
	schemaVersion := big.NewInt(int64(preCommitData.SchemaVersion))

	tx, err := ethereum.SubmitTransactionWithRetries(contract.PreCommit, opts, preCommitData.AnchorID, preCommitData.SigningRoot,
		preCommitData.CentrifugeID, preCommitData.Signature, preCommitData.ExpirationBlock, schemaVersion)

	if err != nil {
		return
	} else {
		log.Infof("Sent off transaction pre-commit [id: %x, hash: %x, SchemaVersion:%v] to registry. Ethereum transaction hash [%x] and Nonce [%v] and Check [%v]", preCommitData.AnchorID,
			preCommitData.SigningRoot, schemaVersion, tx.Hash(), tx.Nonce(), tx.CheckNonce())
	}

	log.Infof("Transfer pending: 0x%x\n", tx.Hash())
	return
}

// sendCommitTransaction sends the actual transaction to register the Anchor on Ethereum registry contract
func sendCommitTransaction(contract AnchorRepositoryContract, opts *bind.TransactOpts, commitData *CommitData) (err error) {
	tx, err := ethereum.SubmitTransactionWithRetries(contract.Commit, opts, commitData.AnchorID.toBigInt(), commitData.DocumentRoot,
		commitData.CentrifugeID.BigInt(), commitData.DocumentProofs, commitData.Signature)

	if err != nil {
		return err
	} else {
		log.Infof("Sent off the anchor [id: %x, hash: %x] to registry. Ethereum transaction hash [%x] and Nonce [%v] and Check [%v]", commitData.AnchorID,
			commitData.DocumentRoot, tx.Hash(), tx.Nonce(), tx.CheckNonce())
	}

	log.Infof("Transfer pending: 0x%x\n", tx.Hash())
	return
}

//CommitAnchor will call the transaction Commit on the smart contract
func (ethRepository *EthereumAnchorRepository) CommitAnchor(anchorID AnchorID, documentRoot DocRoot, centrifugeId identity.CentID, documentProofs [][32]byte, signature []byte) (confirmations <-chan *WatchCommit, err error) {
	ethRepositoryContract, _ := getRepositoryContract()
	opts, err := ethereum.GetConnection().GetTxOpts(config.Config.GetEthereumDefaultAccountName())
	if err != nil {
		return
	}

	commitData := NewCommitData(anchorID, documentRoot, centrifugeId, documentProofs, signature)
	confirmations, err = setUpCommitEventListener(opts.From, commitData)
	if err != nil {
		wError := errors.Wrap(err, 1)
		log.Errorf("Failed to set up event listener for commit transaction [id: %x, hash: %x]: %v",
			commitData.AnchorID, commitData.DocumentRoot, wError)
		return
	}

	err = sendCommitTransaction(ethRepositoryContract, opts, commitData)

	if err != nil {
		wError := errors.Wrap(err, 1)
		log.Errorf("Failed to send Ethereum commit transaction[id: %x, hash: %x, SchemaVersion:%v]: %v",
			commitData.AnchorID, commitData.DocumentRoot, commitData.SchemaVersion, wError)
		return
	}
	return confirmations, err
}

func generateEventContext() (*bind.WatchOpts, context.CancelFunc) {
	//listen to this particular anchor being mined/event is triggered
	ctx, cancelFunc := ethereum.DefaultWaitForTransactionMiningContext()
	watchOpts := &bind.WatchOpts{Context: ctx}

	return watchOpts, cancelFunc

}

// setUpPreCommitEventListener sets up the listened for the "PreCommit" event to notify the upstream code
// about successful mining/creation of a pre-commit.
func setUpPreCommitEventListener(contractEvent WatchAnchorPreCommitted, from common.Address, preCommitData *PreCommitData) (confirmations chan *WatchPreCommit, err error) {

	watchOpts, cancelFunc := generateEventContext()

	//there should always be only one notification coming for this
	//single anchor being registered
	anchorPreCommittedEvents := make(chan *EthereumAnchorRepositoryContractAnchorPreCommitted)
	confirmations = make(chan *WatchPreCommit)
	go waitAndRoutePreCommitEvent(anchorPreCommittedEvents, watchOpts.Context, confirmations, preCommitData)

	//TODO do something with the returned Subscription that is currently simply discarded
	// Somehow there are some possible resource leakage situations with this handling but I have to understand
	// Subscriptions a bit better before writing this code.
	_, err = contractEvent.WatchAnchorPreCommitted(watchOpts, anchorPreCommittedEvents, []common.Address{from}, []*big.Int{preCommitData.AnchorID.toBigInt()})
	if err != nil {
		wError := errors.WrapPrefix(err, "Could not subscribe to event logs for anchor registration", 1)
		log.Errorf("Failed to watch anchor registered event: %v", wError.Error())
		cancelFunc() // cancel the event router
		return confirmations, wError
	}
	return confirmations, nil
}

// setUpCommitEventListener sets up the listened for the "AnchorCommitted" event to notify the upstream code
// about successful mining/creation of a commit
func setUpCommitEventListener(from common.Address, commitData *CommitData) (confirmations chan *WatchCommit, err error) {
	confirmations = make(chan *WatchCommit)
	asyncRes, err := queue.Queue.DelayKwargs(AnchorRepositoryConfirmationTaskName, map[string]interface{}{
		AnchorIDParam:     commitData.AnchorID,
		AddressParam:      from,
		CentrifugeIDParam: commitData.CentrifugeID,
	})
	if err != nil {
		return nil, err
	}
	go waitAndRouteCommitEvent(asyncRes, confirmations, commitData)
	return confirmations, nil
}

// waitAndRoutePreCommitEvent notifies the confirmations channel whenever a pre-commit is being noted as Ethereum event
func waitAndRoutePreCommitEvent(conf <-chan *EthereumAnchorRepositoryContractAnchorPreCommitted, ctx context.Context, confirmations chan<- *WatchPreCommit, preCommitData *PreCommitData) {
	for {
		select {
		case <-ctx.Done():
			log.Errorf("Context [%v] closed before receiving AnchorPreCommitted event for anchor ID: %x, DocumentRoot: %x\n", ctx, preCommitData.AnchorID, preCommitData.SigningRoot)
			confirmations <- &WatchPreCommit{preCommitData, ctx.Err()}
			return
		case res := <-conf:
			log.Infof("Received AnchorPreCommitted event from: %x\n", res.From)
			confirmations <- &WatchPreCommit{preCommitData, nil}
			return
		}
	}
}

// waitAndRouteCommitEvent notifies the confirmations channel whenever a commit is being noted as Ethereum event
func waitAndRouteCommitEvent(asyncResult *gocelery.AsyncResult, confirmations chan<- *WatchCommit, commitData *CommitData) {
	_, err := asyncResult.Get(ethereum.GetDefaultContextTimeout())
	confirmations <- &WatchCommit{commitData, err}
}

func getRepositoryContract() (repositoryContract *EthereumAnchorRepositoryContract, err error) {
	client := ethereum.GetConnection()

	repositoryContract, err = NewEthereumAnchorRepositoryContract(config.Config.GetContractAddress("anchorRepository"), client.GetClient())
	if err != nil {
		log.Fatalf("Failed to instantiate the anchor repository contract: %v", err)
	}
	return
}