require("dotenv").config();
const { default: axios } = require("axios");
const express = require("express");
const appAuth = require("./core/middleware/auth");
const httpRpc = require("./feature/rpc");
const accountsEndpoint = require('./feature/api/account');
var cors = require('cors')
const app = express();
const port = 8000;
app.use(cors())
app.use(express.json());
app.use(appAuth);
app.post("/rpc", httpRpc);
app.get("/", (req, res) => {
  res.status(200).json({ status: "ok" });
});
app.post("/rpc/wallet/balance/bulk", accountsEndpoint.bulkTokenBalance);
app.listen(port, () => {
  console.log(`Example app listening at http://localhost:${port}`);
});
