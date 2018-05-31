package p2pservice

import (
	"testing"
	"github.com/spf13/viper"
	"os"
	"github.com/CentrifugeInc/centrifuge-protobufs/gen/go/coredocument"
	"github.com/CentrifugeInc/centrifuge-protobufs/gen/go/p2p"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/coredocument/repository"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/storage"
	"github.com/CentrifugeInc/go-centrifuge/centrifuge/invoice/repository"
	"context"
	"github.com/stretchr/testify/assert"
)

var dbFileName = "/tmp/centrifuge_testing_p2p_post.leveldb"

func TestMain(m *testing.M) {
	viper.Set("storage.Path", dbFileName)
	defer Bootstrap().Close()

	result := m.Run()
	os.RemoveAll(dbFileName)
	os.Exit(result)
}

func TestP2PService(t *testing.T) {

	identifier := []byte("1")
	coredoc := &coredocumentpb.CoreDocument{DocumentIdentifier: identifier}

	req := p2ppb.P2PMessage{Document: coredoc}
	rpc := P2PService{}
	res, err := rpc.HandleP2PPost(context.Background(), &req)
	assert.Nil(t, err, "Received error")
	assert.Equal(t, res.Document.DocumentIdentifier, identifier, "Incorrect identifier")

	doc, err := coredocumentrepository.GetCoreDocumentRepository().FindById(identifier)
	assert.Equal(t, doc.DocumentIdentifier, identifier, "Document Identifier doesn't match")

}

func Bootstrap() (*leveldb.DB) {
	levelDB := storage.NewLeveldbStorage(dbFileName)

	coredocumentrepository.NewLevelDBCoreDocumentRepository(&coredocumentrepository.LevelDBCoreDocumentRepository{levelDB})
	invoicerepository.NewLevelDBInvoiceRepository(&invoicerepository.LevelDBInvoiceRepository{levelDB})

	return levelDB
}