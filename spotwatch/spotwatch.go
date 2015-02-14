package spotwatch

import (
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/awslabs/aws-sdk-go/aws"
	"github.com/awslabs/aws-sdk-go/service/ec2"
)

//PriceDatum represents a single spot instance price history data point
type PriceDatum struct {
	AvailabilityZone   string
	InstanceType       string
	ProductDescription string
	Price              float64
	Timestamp          time.Time
}

//The following lines enable sorting of PriceDatums by timestamp
type byTimestamp []*PriceDatum

func (b byTimestamp) Len() int           { return len(b) }
func (b byTimestamp) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byTimestamp) Less(i, j int) bool { return b[i].Timestamp.Before(b[j].Timestamp) }

//Policy specifies which region and what instance should be
//monitored. Whenever a new price is detected, the data is sent through
//the channel. Region must be specified
//but all other filtering attributes are optional.
type Policy struct {
	Region             string
	AvailabilityZone   string //TODO: Support multiple availability zones, need secondary sort by AZ after timestamp to match data
	ProductDescription string
	InstanceType       string
	channel            chan *PriceDatum
	done               chan bool
}

//policyData contains the raw price data gathered from a policy.
type policyData struct {
	priceData []*PriceDatum //sorted by timestamp
	sync.RWMutex
}

//Monitor is the main object which users use to interact with the library
type Monitor struct {
	policies map[*Policy]*policyData
	creds    aws.CredentialsProvider
	*sync.RWMutex
	credsLock *sync.Mutex
}

//NewMonitor creates a new spot price monitor
func NewMonitor(creds aws.CredentialsProvider) Monitor {
	var newMonitor Monitor
	newMonitor.policies = make(map[*Policy]*policyData)
	newMonitor.RWMutex = new(sync.RWMutex)
	newMonitor.credsLock = new(sync.Mutex)
	newMonitor.creds = creds
	return newMonitor
}

//AddNewPolicy adds a policy to the monitor, grabs historical data,
//starts monitoring the policy, and will return a channel on which
//it can listen for new prices.
func (m *Monitor) AddNewPolicy(p *Policy) chan *PriceDatum {
	m.Lock()
	defer m.Unlock()
	p.channel = make(chan *PriceDatum, 1000)
	p.done = make(chan bool)
	var data policyData
	data.priceData = make([]*PriceDatum, 0, 1000)
	m.policies[p] = &data
	go m.monitorPolicy(p)
	return p.channel
}

func (m *Monitor) monitorPolicy(p *Policy) {
	//first, get historical data dump
	m.RLock()
	pData := m.policies[p]
	m.RUnlock()
	m.credsLock.Lock()
	cli := ec2.New(m.creds, p.Region, nil)
	m.credsLock.Unlock()
	//Fetch historical data
	pData.Lock()
	log.Println("Fetching historical data!")
	var requestParameters ec2.DescribeSpotPriceHistoryRequest
	requestParameters.InstanceTypes = []string{p.InstanceType}
	requestParameters.ProductDescriptions = []string{p.ProductDescription}
	requestParameters.AvailabilityZone = aws.String(p.AvailabilityZone)
	resp, err := cli.DescribeSpotPriceHistory(&requestParameters)
	if err != nil {
		panic(err)
	}
	for _, spotPrice := range resp.SpotPriceHistory {
		price, err := strconv.ParseFloat(*spotPrice.SpotPrice, 64)
		if err != nil {
			panic(err)
		}
		pData.priceData = append(pData.priceData, &PriceDatum{*spotPrice.AvailabilityZone, *spotPrice.InstanceType, *spotPrice.ProductDescription, price, spotPrice.Timestamp})
	}
	sort.Sort(byTimestamp(pData.priceData))
	pData.Unlock()
	//Now check for new data
	for {
		select {
		case <-p.done:
			close(p.channel)
			p.channel = nil
			break
		case <-time.After(time.Second * 30):
			pData.Lock()
			requestParameters.StartTime = aws.Time(pData.priceData[len(pData.priceData)-1].Timestamp.Add(time.Millisecond * 5))
			resp, err := cli.DescribeSpotPriceHistory(&requestParameters)
			if err != nil {
				panic(err)
			}
			//TODO: should sort incoming price data chronologically, even if the API says it will be sorted.
			for i, spotPrice := range resp.SpotPriceHistory {
				price, err := strconv.ParseFloat(*spotPrice.SpotPrice, 64)
				if err != nil {
					panic(err)
				}
				datum := &PriceDatum{*spotPrice.AvailabilityZone, *spotPrice.InstanceType, *spotPrice.ProductDescription, price, spotPrice.Timestamp}
				//Filter out duplicates, AWS seems to send the current price regardless of the StartTime
				if *pData.priceData[len(pData.priceData)-len(resp.SpotPriceHistory)+i] == *datum {
					continue
				}
				p.channel <- datum
				pData.priceData = append(pData.priceData)
			}
			pData.Unlock()
		}
		if p.channel == nil {
			break
		}
	}
}

func (m *Monitor) removePolicy(p *Policy) {
	m.Lock()
	defer m.Unlock()
	p.done <- true
	close(p.done)
	delete(m.policies, p)
}
