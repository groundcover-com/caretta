FROM alpine:3.17

WORKDIR /app
COPY ./bin/caretta ./

VOLUME /sys/kernel/debug

CMD ./caretta