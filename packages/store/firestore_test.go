package store

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/firestore"
	pb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A local protocol fake exercises the actual SDK adapter without cloud credentials.
type firestoreRPC struct {
	pb.UnimplementedFirestoreServer
	mu           sync.Mutex
	docs         map[string]*pb.Document
	versions     map[string]int
	readVersions map[string]int
}

func (f *firestoreRPC) BeginTransaction(context.Context, *pb.BeginTransactionRequest) (*pb.BeginTransactionResponse, error) {
	return &pb.BeginTransactionResponse{Transaction: []byte("transaction")}, nil
}
func (f *firestoreRPC) Rollback(context.Context, *pb.RollbackRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (f *firestoreRPC) BatchGetDocuments(r *pb.BatchGetDocumentsRequest, stream pb.Firestore_BatchGetDocumentsServer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range r.Documents {
		if len(r.GetTransaction()) > 0 {
			f.readVersions[name] = f.versions[name]
		}
		response := &pb.BatchGetDocumentsResponse{ReadTime: timestamppb.Now()}
		if doc := f.docs[name]; doc != nil {
			response.Result = &pb.BatchGetDocumentsResponse_Found{Found: doc}
		} else {
			response.Result = &pb.BatchGetDocumentsResponse_Missing{Missing: name}
		}
		if err := stream.Send(response); err != nil {
			return err
		}
	}
	return nil
}
func (f *firestoreRPC) Commit(_ context.Context, r *pb.CommitRequest) (*pb.CommitResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := timestamppb.Now()
	results := []*pb.WriteResult{}
	for _, write := range r.Writes {
		doc := write.GetUpdate()
		if doc == nil {
			return nil, status.Error(codes.InvalidArgument, "missing document")
		}
		if len(r.Transaction) > 0 && f.readVersions[doc.Name] != f.versions[doc.Name] {
			return nil, status.Error(codes.Aborted, "concurrent write")
		}
		f.versions[doc.Name]++
		doc.CreateTime = now
		doc.UpdateTime = now
		f.docs[doc.Name] = doc
		results = append(results, &pb.WriteResult{UpdateTime: now})
	}
	return &pb.CommitResponse{CommitTime: now, WriteResults: results}, nil
}

func TestFirestoreSDKCompareAndSwapAndCollectionIsolation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := &firestoreRPC{docs: map[string]*pb.Document{}, versions: map[string]int{}, readVersions: map[string]int{}}
	server := grpc.NewServer()
	pb.RegisterFirestoreServer(server, rpc)
	go server.Serve(listener)
	defer server.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := firestore.NewClient(ctx, "test-project", option.WithEndpoint(listener.Addr().String()), option.WithoutAuthentication(), option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	f := &Firestore{Client: client, Collection: "aria_commerce_test"}
	if err = f.CAS(ctx, "order", nil, []byte("first")); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, "order")
	if err != nil || string(got) != "first" {
		t.Fatal(string(got), err)
	}
	if err = f.CAS(ctx, "order", nil, []byte("replacement")); err != ErrConflict {
		t.Fatal("stale write accepted", err)
	}
	if err = f.CAS(ctx, "order", got, []byte("next")); err != nil {
		t.Fatal(err)
	}
	if err = f.CAS(ctx, "rl_quota", nil, []byte("quota")); err != nil {
		t.Fatal(err)
	}
	ref, _ := f.ref("rl_quota")
	if !strings.Contains(ref.Path, "aria_commerce_test_limits/") {
		t.Fatal("quota mixed with orders", ref.Path)
	}
	if _, err = NewFirestore(ctx, "test", "(default)", "relay_clients"); err == nil {
		t.Fatal("relay collection accepted")
	}
	if _, err = f.ref("../relay_clients"); err != ErrNotFound {
		t.Fatal("escaping record accepted")
	}
}
