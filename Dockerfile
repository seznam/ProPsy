FROM scratch

ADD dist/propsy /propsy

CMD ["/propsy"]
