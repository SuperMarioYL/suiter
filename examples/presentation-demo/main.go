// Exercise each production client's local expired-token check without network.
package main
import (
 "context"
 "encoding/json"
 "fmt"
 "os"
 "strings"
 "github.com/SuperMarioYL/suiter/internal/suite"
 "github.com/SuperMarioYL/suiter/internal/suite/feishu"
 "github.com/SuperMarioYL/suiter/internal/suite/dingtalk"
 "github.com/SuperMarioYL/suiter/internal/suite/wework"
 "github.com/SuperMarioYL/suiter/internal/suite/tencentdocs"
)
func main(){
 expired:=func(context.Context)(suite.Token,error){return suite.Token{AccessToken:"synthetic-expired",ObtainedAt:1,ExpiresIn:1},nil}
 cases:=[]struct{client suite.Suite;kind string}{
 {feishu.NewClient("","").WithTokenGetter(expired),"doc"},
 {dingtalk.NewClient("","").WithTokenGetter(expired),"calendar"},
 {wework.NewClient("","","").WithTokenGetter(expired),"message"},
 {tencentdocs.NewClient("","").WithTokenGetter(expired),"sheet"},
 }
 rows:=[]map[string]string{}
 for _,c:=range cases {_,err:=c.client.Read(context.Background(),c.kind,"synthetic-resource");if err==nil||!strings.Contains(err.Error(),"token expired"){panic(fmt.Sprintf("unexpected result: %v",err))};rows=append(rows,map[string]string{"suite":c.client.Name(),"resource":c.kind,"result":err.Error()})}
 enc:=json.NewEncoder(os.Stdout);enc.SetIndent("","  ");if err:=enc.Encode(rows);err!=nil{panic(err)}
}
