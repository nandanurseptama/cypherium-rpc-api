const { default: axios } = require("axios")
require('dotenv').config()
module.exports = function(req, res, next){
    console.log(process.env.RPC_API)
    console.log(`Body : ${req.body}`);
    axios.post(
        `${process.env.RPC_API}`,
        req.body,
        {
            headers: {'Content-Type': 'application/json'},
        }
    ).then((any)=>{
        return res.status(any.status).json(any.data);
    }).catch((onerror)=>{
	console.log(onerror);
        return res.status(500).json({
            'message' : onerror,
            'data' : null
        });
    });
}
