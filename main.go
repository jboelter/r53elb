// Package r53elb provides a tool to discover the ELB and instances
// associated with a DNS entry in Amazon Route 53.
//
// It uses the Amazon Go SDK (https://github.com/aws/aws-sdk-go) and
// relies on the credentials sourced by the SDK.
//
// See the readme at github.com/jboelter/r53elb for a quick start guide
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/route53"
)

var verbose bool
var debug bool

func main() {
	fmt.Println("Route53 to ELB Instances Lookup Tool")

	var fqdn string

	flag.StringVar(&fqdn, "fqdn", "", "the FQDN to update (e.g. foo.example.com.)")
	flag.BoolVar(&verbose, "verbose", false, "show verbose output")
	flag.BoolVar(&debug, "debug", false, "show aws sdk debug output")

	flag.Parse()

	if len(fqdn) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if fqdn[len(fqdn)-1] != '.' {
		fqdn = fqdn + "."
	}

	fmt.Println("using fqdn", fqdn)

	//create our AWS objects we need to interact w/ AWS APIs
	var awsSession *session.Session
	if debug {
		awsSession = session.New(&aws.Config{LogLevel: aws.LogLevel(aws.LogDebugWithSigning | aws.LogDebugWithHTTPBody)})
	} else {
		awsSession = session.New()
	}
	r53 := route53.New(awsSession)

	// store a map of Domain Name to Hosted Zone ID
	zones := make(map[string]string)
	err := r53.ListHostedZonesPages(&route53.ListHostedZonesInput{},
		// callback func for each page
		func(zz *route53.ListHostedZonesOutput, lastPage bool) bool {
			for _, z := range zz.HostedZones {
				name, id := aws.StringValue(z.Name), strings.TrimPrefix(aws.StringValue(z.Id), "/hostedzone/")

				if _, ok := zones[name]; ok {
					log.Fatal("duplicate domain name entries for", name)
				}

				if verbose {
					fmt.Println(name, id)
				}

				zones[name] = id
			}
			return true
		})
	if err != nil {
		log.Fatal(err)
	}

	zone, prefix, domain := find(fqdn, zones)
	if zone != "" {
		fmt.Printf("found zone %v for %v\n", zone, domain)
	} else {
		fmt.Println("could not find hosted zone for", fqdn)
		return
	}

	var recordSets []*route53.ResourceRecordSet

	err = r53.ListResourceRecordSetsPages(&route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zone),
		// could attempt to filter here (note the reverse name necessary in the API)
	},
		// callback for each page adds it to our recordSets list
		func(rs *route53.ListResourceRecordSetsOutput, lastPage bool) bool {
			for _, r := range rs.ResourceRecordSets {
				if aws.StringValue(r.Name) == fqdn {
					recordSets = append(recordSets, r)
				}
			}
			return true
		})
	if err != nil {
		log.Fatal(err)
	}

	if len(recordSets) == 0 {
		fmt.Printf("No recordset found for %v in zone %v for domain %v\n", prefix, zone, domain)
		return
	}

	aliases := make(map[string][]string)
	// we have all the matching recordSets for our fqdn now
	for _, r := range recordSets {
		if verbose {
			fmt.Println(r)
		}
		if r.AliasTarget != nil && r.AliasTarget.DNSName != nil {

			if !strings.HasSuffix(aws.StringValue(r.AliasTarget.DNSName), "elb.amazonaws.com.") {
				if verbose {
					fmt.Println("skipping", aws.StringValue(r.AliasTarget.DNSName))
				}
				continue // skip
			}

			// expect ELB aliases in the form - http://docs.aws.amazon.com/ElasticLoadBalancing/latest/DeveloperGuide/elb-internet-facing-load-balancers.html
			// name-123456789.region.elb.amazonaws.com
			// ipv6.name-123456789.region.elb.amazonaws.com
			// dualstack.name-123456789.region.elb.amazonaws.com

			l := strings.Split(aws.StringValue(r.AliasTarget.DNSName), ".")
			region := l[len(l)-5] //
			aliases[region] = append(aliases[region], aws.StringValue(r.AliasTarget.DNSName))
		}
	}

	if len(aliases) == 0 {
		fmt.Println("did not find any matching elb resource record sets")
	}

	// range over the ELBs in each region we care about looking for matching DNS Names
	for region, aliasTargets := range aliases {

		if verbose {
			fmt.Println("Checking Load Balancers in region", region)
		}

		awsSession.Config.Region = aws.String(region)

		elbSvc := elb.New(awsSession)
		err = elbSvc.DescribeLoadBalancersPages(&elb.DescribeLoadBalancersInput{
		// optimization -- parse the name out of the alias to filter here
		// LoadBalancerNames: []*string{aws.String("name-goes-here")},
		}, func(lbo *elb.DescribeLoadBalancersOutput, lastPage bool) bool {

			for _, lb := range lbo.LoadBalancerDescriptions {
				dnsName := aws.StringValue(lb.DNSName) + "."
				for _, alias := range aliasTargets {
					if verbose {
						fmt.Printf("checking %v against %v\n", alias, dnsName)
					}
					if strings.HasSuffix(alias, dnsName) {
						//found it
						fmt.Println("---------------------------------")
						fmt.Println("FQDN:     ", fqdn)
						fmt.Println("R53 Alias:", alias)
						fmt.Println("ELB Name: ", aws.StringValue(lb.LoadBalancerName))
						fmt.Println("DNS:      ", aws.StringValue(lb.DNSName))
						fmt.Println("ZoneName: ", aws.StringValue(lb.CanonicalHostedZoneName))
						fmt.Println("ZoneID:   ", aws.StringValue(lb.CanonicalHostedZoneNameID))

						// get the instance health (initating another call from within a callback... sounds bad, but -race is clean)
						var health *elb.DescribeInstanceHealthOutput
						health, err = elbSvc.DescribeInstanceHealth(&elb.DescribeInstanceHealthInput{
							Instances:        lb.Instances,
							LoadBalancerName: lb.LoadBalancerName,
						})
						if err != nil {
							log.Fatal(err)
						}
						for _, i := range health.InstanceStates {
							fmt.Printf("Instance:  %v\t%v\n", aws.StringValue(i.InstanceId), aws.StringValue(i.State))
						}
					}
				}
			}

			return true
		})
		if err != nil {
			log.Fatal(err)
		}
	}
}

// find searches the zones list for a domain portion of the fqdn looking for a longest-first match
func find(fqdn string, zones map[string]string) (zoneID string, prefix string, domain string) {
	var ok bool

	// break foo.example.com. into [foo, example, com, ]
	// note the empty string at the end
	labels := strings.Split(fqdn, ".")

	for i := 0; i < len(labels); i++ {
		if verbose {
			fmt.Println("searching for", strings.Join(labels[i:], "."))
		}
		if zoneID, ok = zones[strings.Join(labels[i:], ".")]; ok {
			prefix = strings.Join(labels[:i], ".")
			domain = strings.Join(labels[i:], ".")
			return
		}
	}

	return "", "", ""
}
