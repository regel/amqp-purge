package main

import (
	"encoding/json"
	"flag"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"github.com/yalp/jsonpath"
	"gopkg.in/go-playground/validator.v9"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	wg sync.WaitGroup

	waitRetryInterval time.Duration
	waitTimeoutFlag   time.Duration

	connString   string // "amqp://" + username + ":" + password + "@" + svc + "/"
	queueName    string
	jsonpathExpr string
	forever      chan string
)

const (
	defaultWaitRetryInterval = time.Second
	defaultConnectionString  = "amqp://guest:guest@localhost:5672/"
	defaultQueueName         = "default"
	defaultJsonpathExpr      = "$.id"
)

func waitForSocket(scheme, addr string, timeout time.Duration) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := net.DialTimeout(scheme, addr, waitTimeoutFlag)
			if err != nil {
				log.Printf("Problem with dial: %v. Sleeping %s\n", err.Error(), waitRetryInterval)
				time.Sleep(waitRetryInterval)
			}
			if conn != nil {
				log.Printf("Connected to %s://%s\n", scheme, addr)
				return
			}
		}
	}()
}

func waitForDependencies(host string, waitTimeoutFlag time.Duration) {
	dependencyChan := make(chan struct{})

	go func() {
		waitForSocket("tcp", host, waitTimeoutFlag)
		wg.Wait()
		close(dependencyChan)
	}()

	select {
	case <-dependencyChan:
		break
	case <-time.After(waitTimeoutFlag):
		log.Fatalf("Timeout after %s waiting on dependencies to become available", waitTimeoutFlag)
	}

}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

// use a single instance of Validate, it caches struct info
var validate *validator.Validate

type PurgeArg struct {
	PurgeId string `validate:"required,alphanum"`
}

func Purge(conn *amqp.Connection, queueName string, val string) {
	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		queueName, // name
		false,     // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	failOnError(err, "Failed to declare a queue")

	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		false,  // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	dones := make(map[string]bool)
	const duration = 1 * time.Second
	timer := time.NewTimer(duration)
L:
	for {
		select {
		case d := <-msgs:
			timer.Reset(duration)
			var field interface{}
			err := json.Unmarshal(d.Body, &field)
			if err != nil {
				log.WithFields(log.Fields{
					"queue": queueName,
				}).Warn("Failed to unmarshal JSON")
				err = d.Nack(false, true)
				failOnError(err, "Failed to Nack message")
				break L
			}
			unmarshaled, err := jsonpath.Read(field, jsonpathExpr)
			if err != nil {
				log.WithFields(log.Fields{
					"queue":    queueName,
					"jsonpath": jsonpathExpr,
				}).Warn("Failed to get JSON field")
				err = d.Nack(false, true)
				failOnError(err, "Failed to Nack message")
				break L
			}
			m := unmarshaled.(string)
			log.WithFields(log.Fields{
				"queue": queueName,
			}).Info("Read")
			if _, ok := dones[m]; ok {
				log.WithFields(log.Fields{
					"value": val,
					"queue": queueName,
				}).Info("Done")
				return
			}
			dones[m] = true
			if m == val {
				err = d.Ack(false)
				failOnError(err, "Failed to Ack message")
				log.WithFields(log.Fields{
					"value": val,
					"queue": queueName,
				}).Info("Deleted")
			} else {
				err = d.Nack(false, true)
				failOnError(err, "Failed to Nack message")
			}

		case <-timer.C:
			log.WithFields(log.Fields{
				"value": val,
				"queue": queueName,
			}).Info("Done (timeout)")
			return
		}
	}
}

func HandlePurge(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	switch req.Method {
	case "POST":
		_, err := ioutil.ReadAll(req.Body)
		if err != nil {
			log.Fatal(err)
		}

	default:
		w.WriteHeader(http.StatusNotImplemented)
		_, err := w.Write([]byte(http.StatusText(http.StatusNotImplemented)))
		failOnError(err, "Failed to write http response")
		return
	}

	id := strings.TrimPrefix(req.URL.Path, "/webhook/purge/")
	arg := &PurgeArg{
		PurgeId: id,
	}

	err := validate.Struct(arg)
	if err != nil {
		clientError := http.StatusBadRequest
		http.Error(w, err.Error(), clientError)
		return
	}

	select {
	case <-ctx.Done():
		err := ctx.Err()
		internalError := http.StatusInternalServerError
		http.Error(w, err.Error(), internalError)
	default:
		forever <- id
		w.WriteHeader(http.StatusNoContent)
	}
}

func main() {
	validate = validator.New()

	flag.DurationVar(&waitTimeoutFlag, "timeout", 10*time.Second, "Host wait timeout")
	flag.DurationVar(&waitRetryInterval, "wait-retry-interval", defaultWaitRetryInterval, "Duration to wait before retrying")
	flag.StringVar(&connString, "connection-string", defaultConnectionString, "AMQP connection string")
	flag.StringVar(&queueName, "queue-name", defaultQueueName, "Consume messages from the given AMQP queue name")
	flag.StringVar(&jsonpathExpr, "jsonpath", defaultJsonpathExpr, "Path of JSON field to read from message queue events")
	flag.Parse()

	if len(os.Getenv("AMQP_CONNECTION_STRING")) > 0 {
		connString = os.Getenv("AMQP_CONNECTION_STRING")
	}
	if len(os.Getenv("AMQP_QUEUE_NAME")) > 0 {
		queueName = os.Getenv("AMQP_QUEUE_NAME")
	}
	if len(os.Getenv("AMQP_JSON_PATH")) > 0 {
		jsonpathExpr = os.Getenv("AMQP_JSON_PATH")
	}

	u, err := url.Parse(connString)
	if err != nil {
		log.Fatalf("bad hostname provided: %s. %s", connString, err.Error())
	}
	waitForDependencies(u.Host, waitTimeoutFlag)
	conn, err := amqp.Dial(connString)
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	notify := conn.NotifyClose(make(chan *amqp.Error)) //error channel

	forever = make(chan string)

	go func() {
		for {
			select {
			case err = <-notify:
				log.WithFields(log.Fields{
					"queue": queueName,
				}).Error("Disconnected")

				os.Exit(1)

			case id := <-forever:
				Purge(conn, queueName, id)
			}
		}
	}()
	http.HandleFunc("/webhook/purge/", HandlePurge)
	err = http.ListenAndServe(":8090", nil)
	failOnError(err, "Failed to listen on port 8090")
}
