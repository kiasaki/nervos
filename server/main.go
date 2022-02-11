package main

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"golang.org/x/crypto/bcrypt"
)

type Item struct {
	ID   int64
	Rev  int64
	Data string
}

type Metadata struct {
	UserHash string
	PassHash []byte
	Chunks   []int64
}

var s3Client *s3.S3
var bucket = os.Getenv("S3_BUCKET")
var userHashRe = regexp.MustCompile("^[a-zA-Z0-9]+$")

func main() {
	gob.Register(Item{})
	gob.Register(Metadata{})

	config := &aws.Config{}
	config.Region = aws.String("us-east-1")
	config.Credentials = credentials.NewStaticCredentials(
		os.Getenv("AWS_ACCESS_KEY"), os.Getenv("AWS_SECRET_KEY"), "")
	s3Session := session.Must(session.NewSession(config))
	s3Client = s3.New(s3Session)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}
	log.Println("start on port", port)
	http.ListenAndServe(":"+port, http.HandlerFunc(handle))
}

func handle(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("panic:", r.Header.Get("userhash"), err)
			w.WriteHeader(500)
		}
	}()

	defer r.Body.Close()
	changes := []Item{}
	check(gob.NewDecoder(r.Body).Decode(&changes))
	userHash := r.Header.Get("userhash")
	passKey, err := hex.DecodeString(r.Header.Get("passkey"))
	if err != nil {
		panic("invalid passkey")
	}
	checkpoint, err := strconv.ParseInt(r.Header.Get("checkpoint"), 10, 64)
	if err != nil {
		panic("invalid checkpoint")
	}

	if !userHashRe.MatchString(userHash) {
		panic("invalid userhash")
	}

	metadata := Metadata{}
	s3GetMetadata(userHash+"/_meta", &metadata)
	if metadata.UserHash == "" {
		metadata.UserHash = userHash
		metadata.PassHash, err = bcrypt.GenerateFromPassword(passKey, 11)
		check(err)
		metadata.Chunks = []int64{0}
		s3Put(userHash+"/_meta", &metadata)
	} else {
		if err := bcrypt.CompareHashAndPassword(metadata.PassHash, passKey); err != nil {
			panic("wrong password")
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Rev < changes[j].Rev
	})
	if len(changes) > 0 {
		chunkKey := fmt.Sprintf("%s/%d", userHash, len(metadata.Chunks)-1)
		chunkItems := []Item{}
		s3GetItems(chunkKey, &chunkItems)
		chunkItems = append(chunkItems, changes...)
		s3Put(chunkKey, &chunkItems)
		if len(chunkItems) >= 500 {
			metadata.Chunks = append(metadata.Chunks, chunkItems[len(chunkItems)-1].Rev)
			s3Put(userHash+"/_meta", &metadata)
		}
	}

	chunkIndex := 0
	for i, rev := range metadata.Chunks {
		if checkpoint < rev {
			chunkIndex = i
		}
	}
	chunkItems := []Item{}
	s3GetItems(fmt.Sprintf("%s/%d", userHash, chunkIndex), &chunkItems)
	items := []Item{}
	for _, i := range chunkItems {
		if i.Rev > checkpoint {
			items = append(items, i)
		}
	}
	if len(items) > 0 {
		checkpoint = items[len(items)-1].Rev
	}
	log.Println("request", userHash, len(changes), len(items), checkpoint)
	w.Header().Set("checkpoint", strconv.FormatInt(checkpoint, 10))
	check(gob.NewEncoder(w).Encode(items))
}

func s3GetMetadata(key string, v *Metadata) {
	bs := s3Get(key)
	if len(bs) > 0 {
		check(gob.NewDecoder(bytes.NewReader(bs)).Decode(v))
	}
}

func s3GetItems(key string, v *[]Item) {
	bs := s3Get(key)
	if len(bs) > 0 {
		check(gob.NewDecoder(bytes.NewReader(bs)).Decode(v))
	}
}

func s3Get(key string) []byte {
	result, err := s3Client.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if aerr, ok := err.(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
		return []byte{}
	}
	check(err)
	defer result.Body.Close()
	bs, err := ioutil.ReadAll(result.Body)
	check(err)
	return bs
}

func s3Put(key string, v interface{}) {
	b := bytes.NewBuffer([]byte{})
	check(gob.NewEncoder(b).Encode(v))
	_, err := s3Client.PutObject(&s3.PutObjectInput{
		ACL:    aws.String("private"),
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(b.Bytes()),
	})
	check(err)
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
