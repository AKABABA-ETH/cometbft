package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"

	"github.com/cometbft/cometbft/v2/abci/types"
	cmtnet "github.com/cometbft/cometbft/v2/internal/net"
	cmtlog "github.com/cometbft/cometbft/v2/libs/log"
	"github.com/cometbft/cometbft/v2/libs/service"
	cmtsync "github.com/cometbft/cometbft/v2/libs/sync"
)

// SocketServer is the server-side implementation of the TSP (Tendermint Socket Protocol)
// for out-of-process go applications. Note, in the case of an application written in golang,
// the developer may also run both Tendermint and the application within the same process.
//
// The socket server deliver.
type SocketServer struct {
	service.BaseService
	isLoggerSet bool

	proto    string
	addr     string
	listener net.Listener

	connsMtx   cmtsync.Mutex
	conns      map[int]net.Conn
	nextConnID int

	appMtx cmtsync.Mutex
	app    types.Application
}

const responseBufferSize = 1000

// NewSocketServer creates a server from a golang-based out-of-process application.
func NewSocketServer(protoAddr string, app types.Application) service.Service {
	proto, addr := cmtnet.ProtocolAndAddress(protoAddr)
	s := &SocketServer{
		proto:    proto,
		addr:     addr,
		listener: nil,
		app:      app,
		conns:    make(map[int]net.Conn),
	}
	s.BaseService = *service.NewBaseService(nil, "ABCIServer", s)
	return s
}

func (s *SocketServer) SetLogger(l cmtlog.Logger) {
	s.BaseService.SetLogger(l)
	s.isLoggerSet = true
}

func (s *SocketServer) OnStart() error {
	ln, err := net.Listen(s.proto, s.addr)
	if err != nil {
		return err
	}

	s.listener = ln
	go s.acceptConnectionsRoutine()

	return nil
}

func (s *SocketServer) OnStop() {
	if err := s.listener.Close(); err != nil {
		s.Logger.Error("Error closing listener", "err", err)
	}

	s.connsMtx.Lock()
	defer s.connsMtx.Unlock()
	for id, conn := range s.conns {
		delete(s.conns, id)
		if err := conn.Close(); err != nil {
			s.Logger.Error("Error closing connection", "id", id, "conn", conn, "err", err)
		}
	}
}

func (s *SocketServer) addConn(conn net.Conn) int {
	s.connsMtx.Lock()
	defer s.connsMtx.Unlock()

	connID := s.nextConnID
	s.nextConnID++
	s.conns[connID] = conn

	return connID
}

// deletes conn even if close errs.
func (s *SocketServer) rmConn(connID int) error {
	s.connsMtx.Lock()
	defer s.connsMtx.Unlock()

	conn, ok := s.conns[connID]
	if !ok {
		return ErrConnectionDoesNotExist{ConnID: connID}
	}

	delete(s.conns, connID)
	return conn.Close()
}

func (s *SocketServer) acceptConnectionsRoutine() {
	for {
		// Accept a connection
		s.Logger.Info("Waiting for new connection...")
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.IsRunning() {
				return // Ignore error from listener closing.
			}
			s.Logger.Error("Failed to accept connection", "err", err)
			continue
		}

		s.Logger.Info("Accepted a new connection")

		connID := s.addConn(conn)

		closeConn := make(chan error, 2)                            // Push to signal connection closed
		responses := make(chan *types.Response, responseBufferSize) // A channel to buffer responses

		// Read requests from conn and deal with them
		go s.handleRequests(closeConn, conn, responses)
		// Pull responses from 'responses' and write them to conn.
		go s.handleResponses(closeConn, conn, responses)

		// Wait until signal to close connection
		go s.waitForClose(closeConn, connID)
	}
}

func (s *SocketServer) waitForClose(closeConn chan error, connID int) {
	err := <-closeConn
	switch {
	case errors.Is(err, io.EOF):
		s.Logger.Error("Connection was closed by client")
	case err != nil:
		s.Logger.Error("Connection error", "err", err)
	default:
		// never happens
		s.Logger.Error("Connection was closed")
	}

	// Close the connection
	if err := s.rmConn(connID); err != nil {
		s.Logger.Error("Error closing connection", "err", err)
	}
}

// Read requests from conn and deal with them.
func (s *SocketServer) handleRequests(closeConn chan error, conn io.Reader, responses chan<- *types.Response) {
	var count int
	bufReader := bufio.NewReader(conn)

	defer func() {
		// make sure to recover from any app-related panics to allow proper socket cleanup.
		// In the case of a panic, we do not notify the client by passing an exception so
		// presume that the client is still running and retrying to connect
		r := recover()
		if r != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			err := fmt.Errorf("recovered from panic: %v\n%s", r, buf)
			if !s.isLoggerSet {
				fmt.Fprintln(os.Stderr, err)
			}
			closeConn <- err
			s.appMtx.Unlock()
		}
	}()

	for {
		req := &types.Request{}
		err := types.ReadMessage(bufReader, req)
		if err != nil {
			if errors.Is(err, io.EOF) {
				closeConn <- err
			} else {
				closeConn <- fmt.Errorf("error reading message: %w", err)
			}
			return
		}
		s.appMtx.Lock()
		count++
		resp, err := s.handleRequest(context.TODO(), req)
		if err != nil {
			// any error either from the application or because of an unknown request
			// throws an exception back to the client. This will stop the server and
			// should also halt the client.
			responses <- types.ToExceptionResponse(err.Error())
		} else {
			responses <- resp
		}
		s.appMtx.Unlock()
	}
}

// handleRequest takes a request and calls the application passing the returned.
func (s *SocketServer) handleRequest(ctx context.Context, req *types.Request) (*types.Response, error) {
	switch r := req.Value.(type) {
	case *types.Request_Echo:
		return types.ToEchoResponse(r.Echo.Message), nil
	case *types.Request_Flush:
		return types.ToFlushResponse(), nil
	case *types.Request_Info:
		res, err := s.app.Info(ctx, r.Info)
		if err != nil {
			return nil, err
		}
		return types.ToInfoResponse(res), nil
	case *types.Request_CheckTx:
		res, err := s.app.CheckTx(ctx, r.CheckTx)
		if err != nil {
			return nil, err
		}
		return types.ToCheckTxResponse(res), nil
	case *types.Request_Commit:
		res, err := s.app.Commit(ctx, r.Commit)
		if err != nil {
			return nil, err
		}
		return types.ToCommitResponse(res), nil
	case *types.Request_Query:
		res, err := s.app.Query(ctx, r.Query)
		if err != nil {
			return nil, err
		}
		return types.ToQueryResponse(res), nil
	case *types.Request_InitChain:
		res, err := s.app.InitChain(ctx, r.InitChain)
		if err != nil {
			return nil, err
		}
		return types.ToInitChainResponse(res), nil
	case *types.Request_FinalizeBlock:
		res, err := s.app.FinalizeBlock(ctx, r.FinalizeBlock)
		if err != nil {
			return nil, err
		}
		return types.ToFinalizeBlockResponse(res), nil
	case *types.Request_ListSnapshots:
		res, err := s.app.ListSnapshots(ctx, r.ListSnapshots)
		if err != nil {
			return nil, err
		}
		return types.ToListSnapshotsResponse(res), nil
	case *types.Request_OfferSnapshot:
		res, err := s.app.OfferSnapshot(ctx, r.OfferSnapshot)
		if err != nil {
			return nil, err
		}
		return types.ToOfferSnapshotResponse(res), nil
	case *types.Request_PrepareProposal:
		res, err := s.app.PrepareProposal(ctx, r.PrepareProposal)
		if err != nil {
			return nil, err
		}
		return types.ToPrepareProposalResponse(res), nil
	case *types.Request_ProcessProposal:
		res, err := s.app.ProcessProposal(ctx, r.ProcessProposal)
		if err != nil {
			return nil, err
		}
		return types.ToProcessProposalResponse(res), nil
	case *types.Request_LoadSnapshotChunk:
		res, err := s.app.LoadSnapshotChunk(ctx, r.LoadSnapshotChunk)
		if err != nil {
			return nil, err
		}
		return types.ToLoadSnapshotChunkResponse(res), nil
	case *types.Request_ApplySnapshotChunk:
		res, err := s.app.ApplySnapshotChunk(ctx, r.ApplySnapshotChunk)
		if err != nil {
			return nil, err
		}
		return types.ToApplySnapshotChunkResponse(res), nil
	case *types.Request_ExtendVote:
		res, err := s.app.ExtendVote(ctx, r.ExtendVote)
		if err != nil {
			return nil, err
		}
		return types.ToExtendVoteResponse(res), nil
	case *types.Request_VerifyVoteExtension:
		res, err := s.app.VerifyVoteExtension(ctx, r.VerifyVoteExtension)
		if err != nil {
			return nil, err
		}
		return types.ToVerifyVoteExtensionResponse(res), nil
	default:
		return nil, ErrUnknownRequest{Request: *req}
	}
}

// Pull responses from 'responses' and write them to conn.
func (*SocketServer) handleResponses(closeConn chan error, conn io.Writer, responses <-chan *types.Response) {
	var count int
	bufWriter := bufio.NewWriter(conn)
	for {
		res := <-responses
		err := types.WriteMessage(res, bufWriter)
		if err != nil {
			closeConn <- fmt.Errorf("error writing message: %w", err)
			return
		}
		if _, ok := res.Value.(*types.Response_Flush); ok {
			err = bufWriter.Flush()
			if err != nil {
				closeConn <- fmt.Errorf("error flushing write buffer: %w", err)
				return
			}
		}

		// If the application has responded with an exception, the server returns the error
		// back to the client and closes the connection. The receiving Tendermint client should
		// log the error and gracefully terminate
		if e, ok := res.Value.(*types.Response_Exception); ok {
			closeConn <- errors.New(e.Exception.Error)
		}
		count++
	}
}
