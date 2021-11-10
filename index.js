require('dotenv').config();
const { default: axios } = require('axios');
const express = require('express');
const appAuth = require('./core/middleware/auth');
const httpRpc = require('./feature/http-rpc');
const app = express()
const port = 3000
app.use(express.json())
app.use(appAuth);
app.post('/rpc',httpRpc);

app.listen(port, () => {
  console.log(`Example app listening at http://localhost:${port}`)
})