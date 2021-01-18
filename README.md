# How to selectively delete messages from an AMQP (RabbitMQ) queue?

You [cannot](https://stackoverflow.com/questions/3434763/how-to-selectively-delete-messages-from-an-amqp-rabbitmq-queue) currently do this in RabbitMQ (or more generally, in AMQP) automatically. But, here's an easy workaround.

# Building amqp-purge from the source

```
make binary
```

## Running amqp-purge

### Running amqp-purge locally

Using default arguments the application will attempt to dial connection to AMQP server running on localhost port 5672, with `guest` username and `guest` password.

```
make run-local
```

### Command line arguments

```
$ ./amqp-purge -h
Usage of ./amqp-purge:
  -connection-string string
    	AMQP connection string (default "amqp://guest:guest@localhost:5672/")
  -jsonpath string
    	Path of JSON field to read from message queue events (default "$.id")
  -queue-name string
    	Consume messages from the given AMQP queue name (default "default")
  -timeout duration
    	Host wait timeout (default 10s)
  -wait-retry-interval duration
    	Duration to wait before retrying (default 1s)
```

### Selectively deleting messages

The application listens on port 8090 for messages to delete.

To delete a single message identified with its JSON key `XYZ`,
send a POST request:

```
$ curl -vvv -X POST "localhost:8090/webhook/purge/XYZ"
*   Trying ::1...
* TCP_NODELAY set
* Connected to localhost (::1) port 8090 (#0)
> POST /webhook/purge/XYZ HTTP/1.1
> Host: localhost:8090
> User-Agent: curl/7.64.1
> Accept: */*
> 
< HTTP/1.1 204 No Content
< Date: Mon, 18 Jan 2021 20:18:36 GMT
< 
* Connection #0 to host localhost left intact
* Closing connection 0
```
