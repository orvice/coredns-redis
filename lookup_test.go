package redis

import (
	"context"
	"strings"
	"testing"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

var zones = []string{
	"example.com.", "example.net.",
}

func TestMain(m *testing.M) {
	passthrough = false
	m.Run()
}

var lookupEntries = [][][]string{
	{
		{"@",
			"{\"soa\":{\"ttl\":300, \"minttl\":100, \"mbox\":\"hostmaster.example.com.\",\"ns\":\"ns1.example.com.\",\"refresh\":44,\"retry\":55,\"expire\":66}}",
		},
		{"x",
			"{\"a\":[{\"ttl\":300, \"ip\":\"1.2.3.4\"},{\"ttl\":300, \"ip\":\"5.6.7.8\"}]," +
				"\"aaaa\":[{\"ttl\":300, \"ip\":\"::1\"}]," +
				"\"txt\":[{\"ttl\":300, \"text\":\"foo\"},{\"ttl\":300, \"text\":\"bar\"}]," +
				"\"ns\":[{\"ttl\":300, \"host\":\"ns1.example.com.\"},{\"ttl\":300, \"host\":\"ns2.example.com.\"}]," +
				"\"mx\":[{\"ttl\":300, \"host\":\"mx1.example.com.\", \"preference\":10},{\"ttl\":300, \"host\":\"mx2.example.com.\", \"preference\":10}]}",
		},
		{"y",
			"{\"cname\":[{\"ttl\":300, \"host\":\"x.example.com.\"}]}",
		},
		{"ns1",
			"{\"a\":[{\"ttl\":300, \"ip\":\"2.2.2.2\"}]}",
		},
		{"ns2",
			"{\"a\":[{\"ttl\":300, \"ip\":\"3.3.3.3\"}]}",
		},
		{"_sip._tcp",
			"{\"srv\":[{\"ttl\":300, \"target\":\"sip.example.com.\",\"port\":555,\"priority\":10,\"weight\":100}]}",
		},
		{"sip",
			"{\"a\":[{\"ttl\":300, \"ip\":\"7.7.7.7\"}]," +
				"\"aaaa\":[{\"ttl\":300, \"ip\":\"::1\"}]}",
		},
	},
	{
		{"@",
			"{\"soa\":{\"ttl\":300, \"minttl\":100, \"mbox\":\"hostmaster.example.net.\",\"ns\":\"ns1.example.net.\",\"refresh\":44,\"retry\":55,\"expire\":66}," +
				"\"ns\":[{\"ttl\":300, \"host\":\"ns1.example.net.\"},{\"ttl\":300, \"host\":\"ns2.example.net.\"}]}",
		},
		{"sub.*",
			"{\"txt\":[{\"ttl\":300, \"text\":\"this is not a wildcard\"}]}",
		},
		{"host1",
			"{\"a\":[{\"ttl\":300, \"ip\":\"5.5.5.5\"}]}",
		},
		{"subdel",
			"{\"ns\":[{\"ttl\":300, \"host\":\"ns1.subdel.example.net.\"},{\"ttl\":300, \"host\":\"ns2.subdel.example.net.\"}]}",
		},
		{"*",
			"{\"txt\":[{\"ttl\":300, \"text\":\"this is a wildcard\"}]," +
				"\"mx\":[{\"ttl\":300, \"host\":\"host1.example.net.\",\"preference\": 10}]}",
		},
		{"_ssh._tcp.host1",
			"{\"srv\":[{\"ttl\":300, \"target\":\"tcp.example.com.\",\"port\":123,\"priority\":10,\"weight\":100}]}",
		},
		{"_ssh._tcp.host2",
			"{\"srv\":[{\"ttl\":300, \"target\":\"tcp.example.com.\",\"port\":123,\"priority\":10,\"weight\":100}]}",
		},
	},
}

var testCases = [][]test.Case{
	// basic tests
	{
		// A Test
		{
			Qname: "x.example.com.", Qtype: dns.TypeA,
			Answer: []dns.RR{
				test.A("x.example.com. 300 IN A 1.2.3.4"),
				test.A("x.example.com. 300 IN A 5.6.7.8"),
			},
		},
		// AAAA Test
		{
			Qname: "x.example.com.", Qtype: dns.TypeAAAA,
			Answer: []dns.RR{
				test.AAAA("x.example.com. 300 IN AAAA ::1"),
			},
		},
		// TXT Test
		{
			Qname: "x.example.com.", Qtype: dns.TypeTXT,
			Answer: []dns.RR{
				test.TXT("x.example.com. 300 IN TXT bar"),
				test.TXT("x.example.com. 300 IN TXT foo"),
			},
		},
		// CNAME Test
		{
			Qname: "y.example.com.", Qtype: dns.TypeCNAME,
			Answer: []dns.RR{
				test.CNAME("y.example.com. 300 IN CNAME x.example.com."),
			},
		},
		// NS Test
		{
			Qname: "x.example.com.", Qtype: dns.TypeNS,
			Answer: []dns.RR{
				test.NS("x.example.com. 300 IN NS ns1.example.com."),
				test.NS("x.example.com. 300 IN NS ns2.example.com."),
			},
			Extra: []dns.RR{
				test.A("ns1.example.com. 300 IN A 2.2.2.2"),
				test.A("ns2.example.com. 300 IN A 3.3.3.3"),
			},
		},
		// MX Test
		{
			Qname: "x.example.com.", Qtype: dns.TypeMX,
			Answer: []dns.RR{
				test.MX("x.example.com. 300 IN MX 10 mx1.example.com."),
				test.MX("x.example.com. 300 IN MX 10 mx2.example.com."),
			},
		},
		// SRV Test
		{
			Qname: "_sip._tcp.example.com.", Qtype: dns.TypeSRV,
			Answer: []dns.RR{
				test.SRV("_sip._tcp.example.com. 300 IN SRV 10 100 555 sip.example.com."),
			},
			Extra: []dns.RR{
				test.A("sip.example.com. 300 IN A 7.7.7.7"),
				test.AAAA("sip.example.com 300 IN AAAA ::1"),
			},
		},
		// NXDOMAIN Test
		{
			Qname: "notexists.example.com.", Qtype: dns.TypeA,
			Rcode: dns.RcodeNameError,
		},
		// SOA Test
		{
			Qname: "example.com.", Qtype: dns.TypeSOA,
			Answer: []dns.RR{
				test.SOA("example.com. 300 IN SOA ns1.example.com. hostmaster.example.com. 1460498836 44 55 66 100"),
			},
		},
	},
	// Wildcard Tests
	{
		{
			Qname: "host3.example.net.", Qtype: dns.TypeMX,
			Answer: []dns.RR{
				test.MX("host3.example.net. 300 IN MX 10 host1.example.net."),
			},
			Extra: []dns.RR{
				test.A("host1.example.net. 300 IN A 5.5.5.5"),
			},
		},
		{
			Qname: "host3.example.net.", Qtype: dns.TypeA,
		},
		{
			Qname: "foo.bar.example.net.", Qtype: dns.TypeTXT,
			Answer: []dns.RR{
				test.TXT("foo.bar.example.net. 300 IN TXT \"this is a wildcard\""),
			},
		},
		{
			Qname: "host1.example.net.", Qtype: dns.TypeMX,
		},
		{
			Qname: "sub.*.example.net.", Qtype: dns.TypeMX,
		},
		{
			Qname: "host.subdel.example.net.", Qtype: dns.TypeA,
			Rcode: dns.RcodeNameError,
		},
		{
			Qname: "ghost.*.example.net.", Qtype: dns.TypeMX,
			Rcode: dns.RcodeNameError,
		},
		{
			Qname: "f.h.g.f.t.r.e.example.net.", Qtype: dns.TypeTXT,
			Answer: []dns.RR{
				test.TXT("f.h.g.f.t.r.e.example.net. 300 IN TXT \"this is a wildcard\""),
			},
		},
	},
}

func newRedisPlugin() *Redis {
	ctxt = context.TODO()

	redis := new(Redis)
	redis.keyPrefix = "coredns:"
	redis.keySuffix = ""
	redis.Ttl = 300
	redis.redisAddress = "10.129.51.131:6379"
	redis.redisPassword = ""
	redis.Connect()
	redis.LoadZones()
	return redis
	/*
		return &Redis {
			keyPrefix: "",
			keySuffix:"",
			redisc: client,
			Ttl: 300,
		}	redis := new(Redis)
	*/
}

func TestAnswer(t *testing.T) {
	log.Info("lookup test")
	r := newRedisPlugin()

	// Test Redis connection
	pong, err := r.Client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Debugf("Redis PING response: %v", pong)

	for i, zone := range zones {
		// Clear existing data for this zone
		pattern := r.keyPrefix + zone + r.keySuffix
		keys, delErr := r.Client.Keys(ctx, pattern).Result()
		if delErr == nil && len(keys) > 0 {
			delReply, err := r.Client.Del(ctx, keys...).Result()
			log.Debugf("Deleted existing keys for zone %s: %v, err: %v", zone, delReply, err)
		}

		// Save test data
		for _, cmd := range lookupEntries[i] {
			err := r.save(zone, cmd[0], cmd[1])
			log.Debugf("Saving %s.%s: %v", cmd[0], zone, err)
			if err != nil {
				log.Error("error in redis", err)
				t.Fail()
			}
		}

		// Verify data was saved correctly
		keys, keysErr := r.Client.HKeys(ctx, r.keyPrefix+zone+r.keySuffix).Result()
		log.Debugf("Keys in Redis for zone %s: %v, err: %v", zone, keys, keysErr)
		for _, tc := range testCases[i] {
			m := tc.Msg()
			log.Debugf("Testing query: %s %s", m.Question[0].Name, dns.TypeToString[m.Question[0].Qtype])

			// Check if the record exists in Redis
			query := strings.TrimSuffix(m.Question[0].Name, "."+zone)
			if query == m.Question[0].Name {
				query = "@"
			}
			recordData, _ := r.Client.HGet(ctx, r.keyPrefix+zone+r.keySuffix, query).Result()
			log.Debugf("Redis data for %s.%s: %s", query, zone, recordData)

			rec := dnstest.NewRecorder(&test.ResponseWriter{})
			r.ServeDNS(ctxt, rec, m)

			resp := rec.Msg
			log.Debugf("DNS response: %+v", resp)

			// TODO(arash): this shouldn't happen, check plugin's empty response
			if resp == nil {
				resp = new(dns.Msg)
			}
			if err := test.SortAndCheck(resp, tc); err != nil {
				t.Errorf("Test failed: %v", err)
			}
		}
	}
}

var ctxt context.Context
