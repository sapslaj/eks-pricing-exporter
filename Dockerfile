FROM alpine:3
WORKDIR /
RUN apk add gcompat
COPY eks-pricing-exporter /bin/eks-pricing-exporter
EXPOSE 9523
CMD ["/bin/eks-pricing-exporter"]
