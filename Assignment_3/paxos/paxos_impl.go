// Partner 1: 2020-10-0287
// Partner 2: 2020-10-0148

package paxos

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"paxosapp/rpc/paxosrpc"
	"sync"
	"time"
)

// proposeTimeout: Time after Propose() fails
var proposeTimeout = 15 * time.Second

// Wait between retires while connecting to server
const retryTimeout = 1 * time.Second

type paxosNode struct {
	listener                                                                                         net.Listener
	srvID, numRetries                                                                                int
	isReplacement                                                                                    bool
	myHostPort                                                                                       string
	connections                                                                                      map[int]*rpc.Client
	lastProposalNums                                                                                 map[int]int                    //latest proposal num for a node
	lastAcceptedValue                                                                                map[string]proposalValueStruct //latest accepted val for a key
	keyProposalMap                                                                                   map[string]int                 //latest accepted proposal num for a key
	committedValues, commitTemp                                                                      map[string]interface{}
	commitLock, connLock, poposalNumsLock, lastAcceptedValueLock, commitTempLock, keyProposalMapLock *sync.Mutex
}

// This struct is a tuple of proposal numbers and values
type proposalValueStruct struct {
	n     int
	value interface{}
}

// NewPaxosNode Desc:
// NewPaxosNode creates a new PaxosNode. This function should return only when
// all nodes have joined the ring, and should return a non-nil error if this node
// could not be started in spite of dialing any other nodes numRetries times.
//
// Params:
// myHostPort: the hostport string of this new node. We use tcp in this project.
//			   	Note: Please listen to this port rather than hostMap[srvId]
// hostMap: a map from all node IDs to their hostports.
//				Note: Please connect to hostMap[srvId] rather than myHostPort
//				when this node try to make rpc call to itself.
// numNodes: the number of nodes in the ring
// numRetries: if we can't connect with some nodes in hostMap after numRetries attempts, an error should be returned
// replace: a flag which indicates whether this node is a replacement for a node which failed.
func NewPaxosNode(myHostPort string, hostMap map[int]string, numNodes, srvID, numRetries int, replace bool) (PaxosNode, error) {
	var node *paxosNode
	node = new(paxosNode)
	node.numRetries = numRetries

	// Start the server
	var err error = nil
	node.listener, err = net.Listen("tcp", myHostPort)
	if err != nil {
		fmt.Println("Error listening:", err)
		return nil, err
	}
	// Register the RPCs
	rpcServer := rpc.NewServer()
	rpcServer.Register(paxosrpc.Wrap(node))
	http.DefaultServeMux = http.NewServeMux()
	rpcServer.HandleHTTP(rpc.DefaultRPCPath, rpc.DefaultDebugPath)
	go http.Serve(node.listener, nil)

	// Make maps and channels
	node.connections = make(map[int]*rpc.Client)
	node.lastProposalNums = make(map[int]int)
	node.keyProposalMap = make(map[string]int)
	node.lastAcceptedValue = make(map[string]proposalValueStruct)
	node.committedValues, node.commitTemp = make(map[string]interface{}), make(map[string]interface{})
	node.keyProposalMapLock, node.commitLock, node.connLock, node.commitTempLock, node.poposalNumsLock, node.lastAcceptedValueLock = &sync.Mutex{}, &sync.Mutex{}, &sync.Mutex{}, &sync.Mutex{}, &sync.Mutex{}, &sync.Mutex{}

	for index, addr := range hostMap {
		for i := 0; i < numRetries; i++ {
			conn, err := rpc.DialHTTP("tcp", addr)
			if err != nil {
				if i == numRetries {
					return nil, errors.New("Could not connect to" + string(index))
				}
				time.Sleep(retryTimeout)
				continue
			} else {
				node.connLock.Lock()
				node.connections[index] = conn
				node.connLock.Unlock()

				break
			}
		}
	}
	node.poposalNumsLock.Lock()
	node.myHostPort = myHostPort
	node.srvID = srvID
	node.lastProposalNums[srvID] = srvID
	node.isReplacement = replace
	node.poposalNumsLock.Unlock()

	if node.isReplacement {

		// Start notifying others that i'm a replacement node
		for _, v := range node.connections {
			var pa *paxosrpc.ReplaceServerArgs = new(paxosrpc.ReplaceServerArgs)
			var pr *paxosrpc.ReplaceServerReply = new(paxosrpc.ReplaceServerReply)
			pa.Hostport = node.myHostPort
			pa.SrvID = node.srvID
			v.Call("PaxosNode.RecvReplaceServer", pa, pr)

		}
		// contact other nodes for their committedValues map information and copy them
		// in my own map
		for _, v := range node.connections {
			var pa *paxosrpc.ReplaceCatchupArgs = new(paxosrpc.ReplaceCatchupArgs)
			var pr *paxosrpc.ReplaceCatchupReply = new(paxosrpc.ReplaceCatchupReply)

			v.Call("PaxosNode.RecvReplaceCatchup", pa, pr)

			tempMap := make(map[string]uint32)
			err := json.Unmarshal(pr.Data, &tempMap)
			if err != nil {
				return nil, err
			}
			node.commitLock.Lock()
			for k, v := range tempMap {
				node.committedValues[k] = v
			}
			node.commitLock.Unlock()
		}
	}
	return node, nil
}

// Desc:
// GetNextProposalNumber generates a proposal number which will be passed to
// Propose. Proposal numbers should not repeat for a key, and for a particular
// <node, key> pair, they should be strictly increasing.
//
// Params:
// args: the key to propose
// reply: the next proposal number for the given key

// Proposal number logic taken from:
// https://stackoverflow.com/questions/47967772/how-to-derive-a-sequence-number-in-paxos
func (pn *paxosNode) GetNextProposalNumber(args *paxosrpc.ProposalNumberArgs, reply *paxosrpc.ProposalNumberReply) error {
	pn.poposalNumsLock.Lock()
	pn.connLock.Lock()
	nextProposalNum := pn.lastProposalNums[pn.srvID]*len(pn.connections) + pn.lastProposalNums[pn.srvID]
	pn.lastProposalNums[pn.srvID] = nextProposalNum
	reply.N = nextProposalNum
	pn.poposalNumsLock.Unlock()
	pn.connLock.Unlock()
	return nil
}

// Desc:
// Propose initializes proposing a value for a key, and replies with the
// value that was committed for that key. Propose should not return until
// a value has been committed, either its own or it waits for
// some other value to be committed (in case it doesn't get a majority) and returns that.
// If proposeTimeout seconds pass then timeout and returns an error.
//

// In case a proposer doesn't get a majority, it starts waiting until theres a value against
// the key in the temporary commit map. Once it gets the value, it returns it and then
// deletes the corresponding value from the map so that the next time a new value is commmitted
// and is put into the map, it ensures the latest committed value is only received.
//
// Params:
// args: the key, value pair to propose together with the proposal number returned by GetNextProposalNumber
// reply: value that was actually committed for the given key

func (pn *paxosNode) proposeWork(args *paxosrpc.ProposeArgs, reply *paxosrpc.ProposeReply, proposeChan chan interface{}) {
	totalOk := 0
	// Tell everyone to prepare
	pn.connLock.Lock()
	for _, otherNode := range pn.connections {
		var pa *paxosrpc.PrepareArgs = new(paxosrpc.PrepareArgs)
		var pr *paxosrpc.PrepareReply = new(paxosrpc.PrepareReply)
		pa.Key = args.Key
		pa.N = args.N
		pa.RequesterId = pn.srvID

		otherNode.Call("PaxosNode.RecvPrepare", pa, pr)
		// Wait for replies from nodes
		if pr.Status == paxosrpc.OK {
			totalOk++
		}
	}
	if totalOk > (len(pn.connections)/2)+1 {
		// We got majority so let's get this accepted
		totalOk = 0
		for _, otherNode := range pn.connections {
			var pa *paxosrpc.AcceptArgs = new(paxosrpc.AcceptArgs)
			var pr *paxosrpc.AcceptReply = new(paxosrpc.AcceptReply)
			pa.Key = args.Key
			pa.N = args.N
			pa.V = args.V
			pa.RequesterId = pn.srvID
			otherNode.Call("PaxosNode.RecvAccept", pa, pr)
			// Wait for replies for accept
			if pr.Status == paxosrpc.OK {
				totalOk++
			}
		}
	} else {
		for {
			time.Sleep(retryTimeout * 2)
			pn.commitTempLock.Lock()
			if v, ok := pn.commitTemp[args.Key]; ok {
				proposeChan <- v
				delete(pn.commitTemp, args.Key)
				pn.commitTempLock.Unlock()
				pn.connLock.Unlock()
				return
			}
			pn.commitTempLock.Unlock()
		}
	}

	if totalOk > (len(pn.connections)/2)+1 {
		totalOk = 0
		// We got majority so let's get this committed
		for _, otherNode := range pn.connections {
			var ca *paxosrpc.CommitArgs = new(paxosrpc.CommitArgs)
			var cr *paxosrpc.CommitReply = new(paxosrpc.CommitReply)
			ca.Key = args.Key
			ca.V = args.V
			ca.RequesterId = pn.srvID
			otherNode.Call("PaxosNode.RecvCommit", ca, cr)
		}
		// Send reply to caller
		proposeChan <- args.V
	} else {
		for {
			time.Sleep(time.Second * 2)
			pn.commitTempLock.Lock()
			if v, ok := pn.commitTemp[args.Key]; ok {
				proposeChan <- v
				delete(pn.commitTemp, args.Key)
				pn.commitTempLock.Unlock()
				pn.connLock.Unlock()
				return
			}
			pn.commitTempLock.Unlock()
		}
	}
	pn.connLock.Unlock()
}

//We have the actual Propose implementation in proposeWork.
//here we call proposeWork in a goroutine and also set a ticker for 15 seconds
//In a select, we wait for a response on either of the two channels i.e. if the RPC has returned,
//on its channel then simply return nil and continue
// else return the timeout error.
func (pn *paxosNode) Propose(args *paxosrpc.ProposeArgs, reply *paxosrpc.ProposeReply) error {
	proposeChan := make(chan interface{})
	ticker := time.NewTicker(proposeTimeout)
	go pn.proposeWork(args, reply, proposeChan)

	select {
	case temp := <-proposeChan:
		reply.V = temp

	case <-ticker.C:
		ticker.Stop()
		return errors.New("Prepare RPC timed out")
	}
	return nil
}

// Desc:
// GetValue looks up the value for a key, and replies with the value or with
// the Status KeyNotFound.
//
// Params:
// args: the key to check
// reply: the value and status for this lookup of the given key
func (pn *paxosNode) GetValue(args *paxosrpc.GetValueArgs, reply *paxosrpc.GetValueReply) error {
	// Check if key exists or not and then reply appropriately
	pn.commitLock.Lock()
	if v, ok := pn.committedValues[args.Key]; ok {
		reply.Status = paxosrpc.KeyFound
		reply.V = v
	} else {
		reply.Status = paxosrpc.KeyNotFound
		reply.V = nil
	}
	pn.commitLock.Unlock()

	return nil
}

// Desc:
// Receive a Prepare message from another Paxos Node. The message contains
// the key whose value is being proposed by the node sending the prepare
// message. This function should respond with Status OK if the prepare is
// accepted and Reject otherwise.
//
// Params:
// args: the Prepare Message, you must include RequesterId when you call this API
// reply: the Prepare Reply Message
func (pn *paxosNode) RecvPrepare(args *paxosrpc.PrepareArgs, reply *paxosrpc.PrepareReply) error {
	// Check if we have seen the value
	pn.keyProposalMapLock.Lock()
	if _, ok := pn.keyProposalMap[args.Key]; ok {
		// If we already accepted a proposal with higher proposal number
		// Then reject this proposal
		if pn.keyProposalMap[args.Key] > args.N {
			reply.Status = paxosrpc.Reject
			// If we have smaller proposal number, then accept it
		} else {
			pn.keyProposalMap[args.Key] = args.N

			pn.lastAcceptedValueLock.Lock()
			reply.N_a = pn.lastAcceptedValue[args.Key].n
			reply.V_a = pn.lastAcceptedValue[args.Key].value
			pn.lastAcceptedValueLock.Unlock()

			reply.Status = paxosrpc.OK
		}
	} else {
		// If we haven't seen this proposal ever, we going to accept it
		reply.N_a = -1
		reply.V_a = nil
		reply.Status = paxosrpc.OK
		pn.keyProposalMap[args.Key] = args.N
	}
	pn.keyProposalMapLock.Unlock()

	return nil
}

// Desc:
// Receive an Accept message from another Paxos Node. The message contains
// the key whose value is being proposed by the node sending the accept
// message. This function should respond with Status OK if the prepare is
// accepted and Reject otherwise.
//
// Params:
// args: the Please Accept Message, you must include RequesterId when you call this API
// reply: the Accept Reply Message
func (pn *paxosNode) RecvAccept(args *paxosrpc.AcceptArgs, reply *paxosrpc.AcceptReply) error {
	// Check if we promised to accept value for this key or not
	pn.keyProposalMapLock.Lock()
	if minProp, ok := pn.keyProposalMap[args.Key]; ok {

		if args.N >= minProp {
			pn.lastAcceptedValueLock.Lock()
			pn.lastAcceptedValue[args.Key] = proposalValueStruct{args.N, args.V}
			pn.lastAcceptedValueLock.Unlock()
			reply.Status = paxosrpc.OK
		} else {
			reply.Status = paxosrpc.Reject
		}
	} else {
		// If we did not promise, then reject
		reply.Status = paxosrpc.Reject
	}
	pn.keyProposalMapLock.Unlock()
	return nil
}

// Desc:
// Receive a Commit message from another Paxos Node. The message contains
// the key whose value was proposed by the node sending the commit
// message.
// Commit the value in another temporary map, this is to signal the waiting proposer
// who didn't get a majority. This way, once it sees that this map has a value
// it means its committed and it can return it.
//
// Params:
// args: the Commit Message, you must include RequesterId when you call this API
// reply: the Commit Reply Message
func (pn *paxosNode) RecvCommit(args *paxosrpc.CommitArgs, reply *paxosrpc.CommitReply) error {
	// Commit the value in our own key value store
	pn.commitLock.Lock()
	pn.committedValues[args.Key] = args.V
	pn.commitTempLock.Lock()
	pn.commitTemp[args.Key] = args.V
	pn.commitTempLock.Unlock()
	pn.commitLock.Unlock()

	return nil
}

// Desc:
// Notify another node of a replacement server which has started up. The
// message contains the Server ID of the node being replaced, and the
// hostport of the replacement node
//

// The receiving server gets to know of the replacement node, dials the connection
// and modifies its connections map accordingly
//
// Params:
// args: the id and the hostport of the server being replaced
// reply: no use
func (pn *paxosNode) RecvReplaceServer(args *paxosrpc.ReplaceServerArgs, reply *paxosrpc.ReplaceServerReply) error {
	for i := 0; i < pn.numRetries; i++ {
		conn, err := rpc.DialHTTP("tcp", args.Hostport)
		if err != nil {
			if i == pn.numRetries {
				break
			}
			time.Sleep(retryTimeout)
			continue
		} else {
			pn.connLock.Lock()
			pn.connections[args.SrvID] = conn
			pn.connLock.Unlock()
			break
		}
	}
	return nil
}

// Desc:
// Request the value that was agreed upon for a particular round. A node
// receiving this message should reply with the data (as an array of bytes)
// needed to make the replacement server aware of the keys and values
// committed so far.
//
// Here we just marshall the committedValues map and send it in the reply
//
//
// Params:
// args: no use
// reply: a byte array containing necessary data used by replacement server to recover
func (pn *paxosNode) RecvReplaceCatchup(args *paxosrpc.ReplaceCatchupArgs, reply *paxosrpc.ReplaceCatchupReply) error {
	pn.commitLock.Lock()
	marshalled, err := json.Marshal(pn.committedValues)
	if err != nil {
		return err
	}
	reply.Data = marshalled
	pn.commitLock.Unlock()

	return nil
}
