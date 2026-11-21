package rag

// metric_integration_test.go 在显式开启时验证指标描述向量召回。
import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMetricVectorRetrievalIntegration(t *testing.T) {
	if os.Getenv("RUN_MILVUS_INTEGRATION") != "1" {
		t.Skip("set RUN_MILVUS_INTEGRATION=1 to run Milvus integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ids, err := RetrieveMetricIDs(ctx, "最近退货太多，退款金额占支付金额多少", 3)
	if err != nil {
		t.Fatalf("RetrieveMetricIDs failed: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("expected metric IDs")
	}
	if ids[0] != "refund_rate" {
		t.Fatalf("expected refund_rate first, got %v", ids)
	}
}

func TestUserDocumentRetrievalIntegration(t *testing.T) {
	if os.Getenv("RUN_MILVUS_INTEGRATION") != "1" {
		t.Skip("set RUN_MILVUS_INTEGRATION=1 to run Milvus integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	datasetID := uuid.NewString()
	chunks := []UserDocumentChunk{
		{
			ID:         datasetID + "_0",
			DatasetID:  datasetID,
			UserID:     1,
			Source:     "refund-policy.md",
			ChunkIndex: 0,
			Content:    "退款率等于退款金额除以支付金额，统计时需要排除取消订单。",
		},
		{
			ID:         datasetID + "_1",
			DatasetID:  datasetID,
			UserID:     1,
			Source:     "shipping-policy.md",
			ChunkIndex: 1,
			Content:    "偏远地区订单通常需要额外两个工作日完成配送。",
		},
	}
	if _, err := IndexUserDocumentChunks(ctx, datasetID, 1, chunks); err != nil {
		t.Fatalf("IndexUserDocumentChunks failed: %v", err)
	}
	defer func() {
		_ = DeleteUserDocuments(context.Background(), datasetID)
	}()

	documents, err := RetrieveUserDocuments(ctx, datasetID, "退款金额怎么算", 2)
	if err != nil {
		t.Fatalf("RetrieveUserDocuments failed: %v", err)
	}
	if len(documents) == 0 {
		t.Fatal("expected user document chunks")
	}
	if documents[0].MetaData["source"] != "refund-policy.md" {
		t.Fatalf("expected refund policy first, got %#v", documents[0].MetaData)
	}
}
