FROM node:20.19-alpine AS builder

WORKDIR /app

COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ .

RUN npm run build


FROM nginx:alpine

WORKDIR /etc/nginx/conf.d

RUN rm default.conf

COPY docker/nginx.conf ./default.conf

COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 4173

CMD ["nginx", "-g", "daemon off;"]
