FROM scratch
COPY openvpn_exporter /bin/openvpn_exporter
ENTRYPOINT ["/bin/openvpn_exporter"]
CMD [ "-h" ]
