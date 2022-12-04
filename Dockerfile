FROM golang:1.19

WORKDIR /app
COPY ./out/caretta ./

VOLUME /sys/kernel/debug

CMD ./caretta