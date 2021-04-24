package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/ristretto"
)

type HttpCacheProxy struct {
	Cache       *ristretto.Cache
	target      *url.URL
	proxy       *httputil.ReverseProxy
	transport   http.RoundTripper
	saveMetrics bool
}

func NewHttpCacheProxy(
	target string,
	expectedItems int,
	cacheMetrics bool,
	maxCacheSize int64,

) (*HttpCacheProxy, error) {
	url, err := url.Parse(target)
	if err != nil {
		return nil, err
	}

	// recommended in ristretto docs
	numCounters := expectedItems * 10

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: int64(numCounters),
		MaxCost:     maxCacheSize,
		BufferItems: 64,
		Metrics:     cacheMetrics,
	})
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(url)
	// overwrite transport
	transport := CachingTransport{cache: cache}
	proxy.Transport = &transport

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Add("X-Cache-Timestamp", fmt.Sprint(time.Now().Unix()))
		return nil
	}

	cachingProxy := HttpCacheProxy{
		Cache:       cache,
		target:      url,
		proxy:       proxy,
		transport:   &transport,
		saveMetrics: cacheMetrics,
	}
	return &cachingProxy, nil
}

func (p *HttpCacheProxy) Handle(wr http.ResponseWriter, req *http.Request) {
	req.Header.Del("X-Forwarded-For")

	if p.saveMetrics {
		m := p.Cache.Metrics
		log.Printf("Metrics: %v", m.String())
	}

	p.proxy.ServeHTTP(wr, req)
}

type CachingTransport struct {
	cache *ristretto.Cache
}

func (t *CachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	useCache := req.Header.Get("X-Cache")
	if uc, err := strconv.ParseBool(useCache); err != nil || !uc {
		return http.DefaultTransport.RoundTrip(req)
	}

	key, err := getKey(req)
	if err != nil {
		return nil, err
	}

	serial, found := t.cache.Get(key)
	if found {
		asBytes := serial.([]byte)
		buf := bufio.NewReader(bytes.NewReader(asBytes))
		return http.ReadResponse(buf, req)
	}

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	var serializedResp bytes.Buffer
	if err := resp.Write(&serializedResp); err != nil {
		return nil, err
	}

	bytes := serializedResp.Bytes()
	if ok := t.cache.Set(key, bytes, int64(len(bytes))); !ok {
		log.Printf("Set cache failed: %v", req.URL.String())
	}

	buf := bufio.NewReader(&serializedResp)
	return http.ReadResponse(buf, req)
}

func getKey(req *http.Request) ([]byte, error) {
	var clonedBody bytes.Buffer
	bodyReader := io.TeeReader(req.Body, &clonedBody)

	var body bytes.Buffer
	_, err := io.Copy(&body, bodyReader)
	req.Body.Close()
	if err != nil {
		return nil, err
	}

	req.Body = io.NopCloser(&body)

	url := req.URL.String()
	key := []byte(url)

	ct := req.Header.Get("Content-Type")
	if isMultipart := strings.HasPrefix(ct, "multipart/"); isMultipart {
		var (
			err error
		)
		mpr, err := req.MultipartReader()
		if err != nil {
			return nil, err
		}

		part, err := mpr.NextPart()
		for err == nil {
			partBytes, pErr := io.ReadAll(part)
			if pErr != nil {
				return nil, pErr
			}

			key = append(key, partBytes...)

			part.Close()

			part, err = mpr.NextPart()
		}
		req.Body.Close()
		req.Body = io.NopCloser(&clonedBody)
		return key, nil
	}

	key = append(key, clonedBody.Bytes()...)
	return key, nil
}
