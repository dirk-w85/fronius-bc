FROM golang:1.24.5-alpine
WORKDIR /app
#COPY . .
#RUN go mod download
#RUN go build -o main .
env GOPROXY=direct
RUN apk update
RUN apk add git

#CMD ["./main"]