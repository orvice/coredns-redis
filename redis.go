package redis

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/coredns/coredns/plugin"

	clog "github.com/coredns/coredns/plugin/pkg/log"
	goredis "github.com/redis/go-redis/v9"
)

var log = clog.NewWithPlugin("redis-plugin")
var ctx = context.Background()

type Redis struct {
	Next           plugin.Handler
	Client         *goredis.Client
	redisAddress   string
	redisPassword  string
	connectTimeout int
	readTimeout    int
	keyPrefix      string
	keySuffix      string
	Ttl            uint32
	Zones          []string
	LastZoneUpdate time.Time
}

func (redis *Redis) LoadZones() {
	log.Info("Start Loading zones from redis")

	if redis.Client == nil {
		clog.Error("error connecting to redis")
		return
	}

	pattern := redis.keyPrefix + "*" + redis.keySuffix
	keys, err := redis.Client.Keys(ctx, pattern).Result()
	if err != nil {
		log.Error("error getting keys from redis ", " redis addr: ", redis.redisAddress, " error ", err.Error())
		return
	}

	zones := make([]string, len(keys))
	for i, key := range keys {
		zones[i] = strings.TrimPrefix(key, redis.keyPrefix)
		zones[i] = strings.TrimSuffix(zones[i], redis.keySuffix)
	}

	redis.LastZoneUpdate = time.Now()
	redis.Zones = zones
}

func (redis *Redis) A(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, a := range record.A {
		if a.Ip == nil {
			continue
		}
		r := new(dns.A)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeA,
			Class: dns.ClassINET, Ttl: redis.minTtl(a.Ttl)}
		r.A = a.Ip
		answers = append(answers, r)
	}
	return
}

func (redis Redis) AAAA(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, aaaa := range record.AAAA {
		if aaaa.Ip == nil {
			continue
		}
		r := new(dns.AAAA)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeAAAA,
			Class: dns.ClassINET, Ttl: redis.minTtl(aaaa.Ttl)}
		r.AAAA = aaaa.Ip
		answers = append(answers, r)
	}
	return
}

func (redis *Redis) CNAME(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, cname := range record.CNAME {
		if len(cname.Host) == 0 {
			continue
		}
		r := new(dns.CNAME)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCNAME,
			Class: dns.ClassINET, Ttl: redis.minTtl(cname.Ttl)}
		r.Target = dns.Fqdn(cname.Host)
		answers = append(answers, r)
	}
	return
}

func (redis *Redis) TXT(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, txt := range record.TXT {
		if len(txt.Text) == 0 {
			continue
		}
		r := new(dns.TXT)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeTXT,
			Class: dns.ClassINET, Ttl: redis.minTtl(txt.Ttl)}
		r.Txt = split255(txt.Text)
		answers = append(answers, r)
	}
	return
}

func (redis *Redis) NS(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, ns := range record.NS {
		if len(ns.Host) == 0 {
			continue
		}
		r := new(dns.NS)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeNS,
			Class: dns.ClassINET, Ttl: redis.minTtl(ns.Ttl)}
		r.Ns = ns.Host
		answers = append(answers, r)
		extras = append(extras, redis.hosts(ns.Host, z)...)
	}
	return
}

func (redis *Redis) MX(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, mx := range record.MX {
		if len(mx.Host) == 0 {
			continue
		}
		r := new(dns.MX)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeMX,
			Class: dns.ClassINET, Ttl: redis.minTtl(mx.Ttl)}
		r.Mx = mx.Host
		r.Preference = mx.Preference
		answers = append(answers, r)
		extras = append(extras, redis.hosts(mx.Host, z)...)
	}
	return
}

func (redis *Redis) SRV(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	for _, srv := range record.SRV {
		if len(srv.Target) == 0 {
			continue
		}
		r := new(dns.SRV)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeSRV,
			Class: dns.ClassINET, Ttl: redis.minTtl(srv.Ttl)}
		r.Target = srv.Target
		r.Weight = srv.Weight
		r.Port = srv.Port
		r.Priority = srv.Priority
		answers = append(answers, r)
		extras = append(extras, redis.hosts(srv.Target, z)...)
	}
	return
}

func (redis *Redis) SOA(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	r := new(dns.SOA)
	if record.SOA.Ns == "" {
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeSOA,
			Class: dns.ClassINET, Ttl: redis.Ttl}
		r.Ns = "ns1." + name
		r.Mbox = "hostmaster." + name
		r.Refresh = 86400
		r.Retry = 7200
		r.Expire = 3600
		r.Minttl = redis.Ttl
	} else {
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(z.Name), Rrtype: dns.TypeSOA,
			Class: dns.ClassINET, Ttl: redis.minTtl(record.SOA.Ttl)}
		r.Ns = record.SOA.Ns
		r.Mbox = record.SOA.MBox
		r.Refresh = record.SOA.Refresh
		r.Retry = record.SOA.Retry
		r.Expire = record.SOA.Expire
		r.Minttl = record.SOA.MinTtl
	}
	r.Serial = redis.serial()
	answers = append(answers, r)
	return
}

func (redis *Redis) CAA(name string, z *Zone, record *Record) (answers, extras []dns.RR) {
	if record == nil {
		return
	}
	for _, caa := range record.CAA {
		if caa.Value == "" || caa.Tag == "" {
			continue
		}
		r := new(dns.CAA)
		r.Hdr = dns.RR_Header{Name: dns.Fqdn(name), Rrtype: dns.TypeCAA, Class: dns.ClassINET}
		r.Flag = caa.Flag
		r.Tag = caa.Tag
		r.Value = caa.Value
		answers = append(answers, r)
	}
	return
}

func (redis *Redis) AXFR(z *Zone) (records []dns.RR) {
	//soa, _ := redis.SOA(z.Name, z, record)
	soa := make([]dns.RR, 0)
	answers := make([]dns.RR, 0, 10)
	extras := make([]dns.RR, 0, 10)

	// Allocate slices for rr Records
	records = append(records, soa...)
	for key := range z.Locations {
		if key == "@" {
			location := redis.findLocation(z.Name, z)
			record := redis.get(location, z)
			soa, _ = redis.SOA(z.Name, z, record)
		} else {
			fqdnKey := dns.Fqdn(key) + z.Name
			var as []dns.RR
			var xs []dns.RR

			location := redis.findLocation(fqdnKey, z)
			record := redis.get(location, z)

			// Pull all zone records
			as, xs = redis.A(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)

			as, xs = redis.AAAA(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)

			as, xs = redis.CNAME(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)

			as, xs = redis.MX(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)

			as, xs = redis.SRV(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)

			as, xs = redis.TXT(fqdnKey, z, record)
			answers = append(answers, as...)
			extras = append(extras, xs...)
		}
	}

	records = soa
	records = append(records, answers...)
	records = append(records, extras...)
	records = append(records, soa...)

	log.Error(records)
	return
}

func (redis *Redis) hosts(name string, z *Zone) []dns.RR {
	var (
		record  *Record
		answers []dns.RR
	)
	location := redis.findLocation(name, z)
	if location == "" {
		return nil
	}
	record = redis.get(location, z)
	a, _ := redis.A(name, z, record)
	answers = append(answers, a...)
	aaaa, _ := redis.AAAA(name, z, record)
	answers = append(answers, aaaa...)
	cname, _ := redis.CNAME(name, z, record)
	answers = append(answers, cname...)
	return answers
}

func (redis *Redis) serial() uint32 {
	return uint32(time.Now().Unix())
}

func (redis *Redis) minTtl(ttl uint32) uint32 {
	if redis.Ttl == 0 && ttl == 0 {
		return defaultTtl
	}
	if redis.Ttl == 0 {
		return ttl
	}
	if ttl == 0 {
		return redis.Ttl
	}
	if redis.Ttl < ttl {
		return redis.Ttl
	}
	return ttl
}

func (redis *Redis) findLocation(query string, z *Zone) string {
	var (
		ok                                 bool
		closestEncloser, sourceOfSynthesis string
	)

	// request for zone records
	if query == z.Name {
		return query
	}

	query = strings.TrimSuffix(query, "."+z.Name)

	if _, ok = z.Locations[query]; ok {
		return query
	}

	closestEncloser, sourceOfSynthesis, ok = splitQuery(query)
	for ok {
		ceExists := keyMatches(closestEncloser, z) || keyExists(closestEncloser, z)
		ssExists := keyExists(sourceOfSynthesis, z)
		if ceExists {
			if ssExists {
				return sourceOfSynthesis
			} else {
				return ""
			}
		} else {
			closestEncloser, sourceOfSynthesis, ok = splitQuery(closestEncloser)
		}
	}
	return ""
}

func (redis *Redis) get(key string, z *Zone) *Record {
	if redis.Client == nil {
		log.Error("error connecting to redis")
		return nil
	}

	var label string
	if key == z.Name {
		label = "@"
	} else {
		label = key
	}

	val, err := redis.Client.HGet(ctx, redis.keyPrefix+z.Name+redis.keySuffix, label).Result()
	if err != nil {
		if err != goredis.Nil {
			log.Error("error getting key from redis", err.Error())
		}
		return nil
	}

	r := new(Record)
	err = json.Unmarshal([]byte(val), r)
	if err != nil {
		log.Error("parse error : ", val, err)
		return nil
	}
	return r
}

func keyExists(key string, z *Zone) bool {
	_, ok := z.Locations[key]
	return ok
}

func keyMatches(key string, z *Zone) bool {
	for value := range z.Locations {
		if strings.HasSuffix(value, key) {
			return true
		}
	}
	return false
}

func splitQuery(query string) (string, string, bool) {
	if query == "" {
		return "", "", false
	}
	var (
		splits            []string
		closestEncloser   string
		sourceOfSynthesis string
	)
	splits = strings.SplitAfterN(query, ".", 2)
	if len(splits) == 2 {
		closestEncloser = splits[1]
		sourceOfSynthesis = "*." + closestEncloser
	} else {
		closestEncloser = ""
		sourceOfSynthesis = "*"
	}
	return closestEncloser, sourceOfSynthesis, true
}

func (redis *Redis) Connect() {
	log.Info("connect to redis ", redis.redisAddress)
	opts := &goredis.Options{
		Addr: redis.redisAddress,
	}

	if redis.redisPassword != "" {
		opts.Password = redis.redisPassword
	}

	if redis.connectTimeout != 0 {
		opts.DialTimeout = time.Duration(redis.connectTimeout) * time.Millisecond
	}

	if redis.readTimeout != 0 {
		opts.ReadTimeout = time.Duration(redis.readTimeout) * time.Millisecond
	}

	redis.Client = goredis.NewClient(opts)

	if err := redis.Client.Ping(ctx).Err(); err != nil {
		log.Error("error on ping to redis", err.Error())
		return
	}
}

func (redis *Redis) save(zone string, subdomain string, value string) error {
	if redis.Client == nil {
		log.Error("error connecting to redis")
		return nil
	}

	_, err := redis.Client.HSet(ctx, redis.keyPrefix+zone+redis.keySuffix, subdomain, value).Result()
	return err
}

func (redis *Redis) load(zone string) *Zone {
	if redis.Client == nil {
		log.Error("error connecting to redis, redis is nil")
		return nil
	}

	vals, err := redis.Client.HKeys(ctx, redis.keyPrefix+zone+redis.keySuffix).Result()
	if err != nil {
		log.Error("error getting keys from redis", err.Error())
		return nil
	}

	z := new(Zone)
	z.Name = zone
	z.Locations = make(map[string]struct{})
	for _, val := range vals {
		z.Locations[val] = struct{}{}
	}

	return z
}

func split255(s string) []string {
	if len(s) < 255 {
		return []string{s}
	}
	sx := []string{}
	p, i := 0, 255
	for {
		if i <= len(s) {
			sx = append(sx, s[p:i])
		} else {
			sx = append(sx, s[p:])
			break

		}
		p, i = p+255, i+255
	}

	return sx
}

const (
	defaultTtl     = 360
	hostmaster     = "hostmaster"
	zoneUpdateTime = 10 * time.Second
	transferLength = 1000
)
