package security

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bastio-ai/bastio/pkg/gatewaypolicy"
)

func TestGatewayMonitoringAndMetadataOnlyRetainNoMatchedContent(t *testing.T){
	const secret="synthetic-person@example.com"
	live:=&ScanResult{Findings:[]Finding{{MatchedContent:secret,Message:"found "+secret,Action:ActionBlock}},ShouldBlock:true,Action:ActionBlock,SanitizedContent:"masked"}
	ctx:=gatewaypolicy.With(context.Background(),gatewaypolicy.Policy{MonitorOnly:true,OmitContent:true})
	ObserveGatewayResult(ctx,live,secret)
	if live.ShouldBlock||live.Action!=ActionLogOnly||live.SanitizedContent!=secret{t.Fatal("monitoring rewrote or blocked")}
	retained:=MetadataOnlyResult(live);data,_:=json.Marshal(retained)
	if strings.Contains(string(data),secret){t.Fatal("matched payload escaped into retained findings")}
	if len(retained.Findings)!=1||live.Findings[0].MatchedContent!=secret{t.Fatal("metadata removal changed active inspection")}
}
