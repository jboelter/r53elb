[![Build Status](https://travis-ci.org/jboelter/r53elb.png?branch=master)](https://travis-ci.org/jboelter/r53elb)

r53elb provides a tool to lookup the EC2 Instances servicing an ELB from a Route53 entr.  You provide the fqdn to lookup in Route53 (a record you have created on one of your Hosted Zones) and the tool will attempt to find the ELB and Instances servicing that route.  

Note: I've been tinkering with the Amazon Go SDK (instead of the Crowdmob version) and this was put together quite quickly in reponse to a [Reddit thread](https://www.reddit.com/r/aws/comments/4pai04/setting_up_a_simple_page_to_unmask_instances/) as a fun experiment (and turns out, useful tool for myself). It could be optimized to limit the data returned from AWS by filtering and/or caching. 

It uses the Amazon Go SDK (https://github.com/aws/aws-sdk-go) and relies on the credentials sourced by the SDK.  It is not necessary to specify a region (AWS_REGION), it is set automatically for the calls to discover ELBs.

Dependencies

    go get -u github.com/aws/aws-sdk-go/aws
    or
    gvt restore

Limitations:

    The same account credentials are used for all API calls

QuickStart:

    AWS_ACCESS_KEY_ID=xxx AWS_SECRET_ACCESS_KEY=zzz r53elb
    --fqdn x.sub.example.com

Output:

    Route53 to HostedZone ELB to Instances Lookup Tool
    FQDN: x.sub.example.com.
    found zone Z21AQR372GP4VT for sub.example.com.
    ---------------------------------
    FQDN:      x.sub.example.com.
    R53 Alias: dualstack.your-elb-name-1234567890.us-west-2.elb.amazonaws.com.
    ELB Name:  your-elb-name
    DNS:       your-elb-name-1234567890.us-west-2.elb.amazonaws.com
    ZoneName:  your-elb-name-1234567890.us-west-2.elb.amazonaws.com
    ZoneID:    Z21AQR372GP4VT
    Instance:  i-b497c9ee	InService
    Instance:  i-a80a7297	InService
    Instance:  i-ea5ccef3	InService

Theory of Operation:

 - Fetch the Hosted Zones (domain name + zone ID) with `ListHostedZonesPages`
 - Search the domain names for our FQDN, longest match preferred
 - Fetch the Resource Records Sets for the matching Zone ID with `ListResourceRecordSetsPages`
 - If the Route 53 Resource Record Set Name field matches the fqdn, keep it
 - Iterate through each matching Record Set and determine if it is an Alias to an ELB. This stores each alias in a map by Region. This step could be combined w/ the callback above
 - Fetch the Load Balancers for each Region we care about (in the map) and check if any of them resolve our Alias with `DescribeLoadBalancersPages`
 - When we get a match; dump the details including a call to `DescribeInstanceHealth`



