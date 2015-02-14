#SpotWatch

SpotWatch is a library which helps to monitor AWS spot instance prices. Usage is split into several stages:

1. First, define a *policy*: an object with attributes of the class of spot instances that you want to watch.
2. Second, create a *monitor*, the object which you will use to interface with the library.
3. Third, add your policy to the monitor with `addNewPolicy`. This method will return a channel with new price updates. 
4. Last, monitor the returned channel for new prices, and perform statistics on the policy's historical data.
5. 


**The API of the library is still in flux!** In addition, the library *will crash* without a version of the `aws-sdk-go` library with several fixes I made (in addition, the repo has to be on the develop branch). I'll create a fork of that repo if it isn't fixed soon.

##Example program

```go
package main

import (
	"fmt"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/codecombat/nucleator/spotwatch"
)

func main() {
	creds := aws.EnvCreds()
	monitor := spot.NewMonitor(creds)
	var policy spot.Policy
	policy.InstanceType = "c3.xlarge"
	policy.ProductDescription = "Linux/UNIX"
	policy.AvailabilityZone = "us-east-1b"
	policy.Region = "us-east-1"
	policyChannel := monitor.AddNewPolicy(&policy)
	spotPrice := <-policyChannel
}
```
