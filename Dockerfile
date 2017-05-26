FROM golang AS build-env
ADD . src/github.com/okzk/slack-approver
WORKDIR src/github.com/okzk/slack-approver

RUN sh -c 'curl https://glide.sh/get | sh' \
  && glide install -v \
  && CGO_ENABLED=0 go build

FROM alpine

RUN apk add --no-cache ca-certificates
COPY --from=build-env /go/src/github.com/okzk/slack-approver/slack-approver /usr/local/bin/
EXPOSE 8080
CMD ["slack-approver"]
