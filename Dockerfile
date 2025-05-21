##
## Build
##

# pull official base image
FROM golang:alpine as build-env

# set work directory
WORKDIR /go/check_mssql

# copy project from local
COPY . /go/check_mssql

# get modules
RUN go mod download

# build sla_exporter binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -gcflags "all=-N -l" -o ./build/check_mssql


##
## Deploy
##

# pull official base image
FROM golang:alpine

# set work directory
WORKDIR /go/check_mssql

# copy binary from build-env container
COPY --from=build-env /go/check_mssql/build/check_mssql ./

# run binary
CMD ["./check_mssql"]
