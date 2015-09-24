#
# Usage example:
# $ docker build -t ssh-chat .
# $ docker run -d -p 0.0.0.0:(your host machine port):2022 --name ssh-chat ssh-chat
#
FROM busybox
MAINTAINER Alvin Lai <al@alvinlai.com>

ADD ssh-chat ssh-chat
ADD id_rsa id_rsa

EXPOSE 2022

CMD ["/ssh-chat", "-i", "id_rsa", "-vv", "--bind", "\":2022\""]
# ENTRYPOINT ["ssh-chat"]
